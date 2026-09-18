package zengindata

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

// SourceArchiveURL は zengin-code/source-data のデフォルトブランチを取得するアーカイブURL。
//
// このリポジトリは GitHub Releases を作っておらず、GitHub Actions が毎日 master ブランチの
// data/*.json を更新する運用になっている（README のバッジ参照）。そのため git clone/pull で
// 追随するのではなく、tarball アーカイブを毎回まるごと取得する方式を採用した。
// git バイナリへの依存も、.git ディレクトリの永続化も不要になり、更新処理は
// 「ダウンロードして展開し、成功したらアトミックに置き換える」だけの冪等な操作にできる。
const SourceArchiveURL = "https://codeload.github.com/zengin-code/source-data/tar.gz/refs/heads/master"

const fetchTimeout = 2 * time.Minute

var branchFileRe = regexp.MustCompile(`^[^/]+/data/branches/(\d{4})\.json$`)

// UpdateResult は Update の実行結果サマリ。
type UpdateResult struct {
	BankCount       int
	BranchFileCount int
	BranchCount     int
	SourceUpdatedAt string
}

// Update は zengin-code/source-data から最新データを取得し、dataDir 配下に
// banks.json / branches/{bank_code}.json / updated_at をアトミックに書き出す。
func Update(ctx context.Context, dataDir string) (UpdateResult, error) {
	return UpdateFrom(ctx, dataDir, SourceArchiveURL)
}

// UpdateFrom は取得元URLを差し替え可能にした Update。テストや代替ミラーからの取得に使う。
func UpdateFrom(ctx context.Context, dataDir, archiveURL string) (UpdateResult, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	body, err := downloadArchive(ctx, archiveURL)
	if err != nil {
		return UpdateResult{}, fmt.Errorf("zengindata: アーカイブ取得に失敗しました: %w", err)
	}
	defer body.Close()

	banks, branchesByBank, updatedAt, err := extractArchive(body)
	if err != nil {
		return UpdateResult{}, fmt.Errorf("zengindata: アーカイブ展開に失敗しました: %w", err)
	}
	if len(banks) == 0 {
		return UpdateResult{}, fmt.Errorf("zengindata: アーカイブ内に data/banks.json が見つかりませんでした")
	}

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return UpdateResult{}, fmt.Errorf("zengindata: データディレクトリ作成に失敗しました: %w", err)
	}

	if err := writeJSONAtomic(filepath.Join(dataDir, "banks.json"), banks); err != nil {
		return UpdateResult{}, fmt.Errorf("zengindata: banks.json 書き込みに失敗しました: %w", err)
	}

	branchesDir := filepath.Join(dataDir, "branches")
	if err := os.MkdirAll(branchesDir, 0o755); err != nil {
		return UpdateResult{}, fmt.Errorf("zengindata: branches ディレクトリ作成に失敗しました: %w", err)
	}

	branchCount := 0
	for bankCode, branches := range branchesByBank {
		out := filepath.Join(branchesDir, bankCode+".json")
		if err := writeJSONAtomic(out, branches); err != nil {
			return UpdateResult{}, fmt.Errorf("zengindata: %s 書き込みに失敗しました: %w", out, err)
		}
		branchCount += len(branches)
	}

	if updatedAt != "" {
		if err := os.WriteFile(filepath.Join(dataDir, "updated_at"), []byte(updatedAt), 0o644); err != nil {
			return UpdateResult{}, fmt.Errorf("zengindata: updated_at 書き込みに失敗しました: %w", err)
		}
	}

	return UpdateResult{
		BankCount:       len(banks),
		BranchFileCount: len(branchesByBank),
		BranchCount:     branchCount,
		SourceUpdatedAt: updatedAt,
	}, nil
}

func downloadArchive(ctx context.Context, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("リクエスト生成に失敗しました: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTPリクエストに失敗しました: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("想定外のステータスコード: %s", resp.Status)
	}
	return resp.Body, nil
}

// extractArchive は tar.gz を展開しつつ banks.json / branches/*.json を読み取る。
// tarエントリのパスは "<repo>-<ref>/..." というプレフィックスが付くため、先頭ディレクトリは無視する。
func extractArchive(r io.Reader) (banks []Bank, branchesByBank map[string][]Branch, updatedAt string, err error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, nil, "", fmt.Errorf("gzip展開に失敗しました: %w", err)
	}
	defer gz.Close()

	branchesByBank = make(map[string][]Branch)
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, "", fmt.Errorf("tar読み取りに失敗しました: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}

		name := path.Clean(hdr.Name)
		switch {
		case matchesRepoFile(name, "data/banks.json"):
			raw := make(map[string]Bank)
			if err := json.NewDecoder(tr).Decode(&raw); err != nil {
				return nil, nil, "", fmt.Errorf("banks.json デコードに失敗しました: %w", err)
			}
			banks = mapToSortedBanks(raw)
		case matchesRepoFile(name, "data/updated_at"):
			b, err := io.ReadAll(tr)
			if err != nil {
				return nil, nil, "", fmt.Errorf("updated_at 読み取りに失敗しました: %w", err)
			}
			updatedAt = string(b)
		default:
			if m := branchFileRe.FindStringSubmatch(name); m != nil {
				bankCode := m[1]
				raw := make(map[string]Branch)
				if err := json.NewDecoder(tr).Decode(&raw); err != nil {
					return nil, nil, "", fmt.Errorf("%s デコードに失敗しました: %w", name, err)
				}
				branchesByBank[bankCode] = mapToSortedBranches(bankCode, raw)
			}
		}
	}

	return banks, branchesByBank, updatedAt, nil
}

// matchesRepoFile は "<repo>-<ref>/" プレフィックスを除いたパスが suffix と一致するかを見る。
func matchesRepoFile(name, suffix string) bool {
	idx := indexByte(name, '/')
	if idx < 0 {
		return false
	}
	return name[idx+1:] == suffix
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func mapToSortedBanks(raw map[string]Bank) []Bank {
	out := make([]Bank, 0, len(raw))
	for code, b := range raw {
		if b.Code == "" {
			b.Code = code
		}
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}

func mapToSortedBranches(bankCode string, raw map[string]Branch) []Branch {
	out := make([]Branch, 0, len(raw))
	for code, b := range raw {
		if b.Code == "" {
			b.Code = code
		}
		b.BankCode = bankCode
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}

func writeJSONAtomic(destPath string, v interface{}) error {
	tmp := destPath + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, destPath)
}
