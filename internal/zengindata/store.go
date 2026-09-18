package zengindata

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
)

// snapshot はある時点でのマスタデータ全体。Store は常にこれをアトミックに丸ごと差し替える。
type snapshot struct {
	banks          []Bank
	bankByCode     map[string]Bank
	branchesByBank map[string][]Branch
	branchByKey    map[branchKey]Branch
	updatedAt      string
}

type branchKey struct {
	bankCode string
	code     string
}

// Store はメモリ上に保持するマスタデータの読み取り専用ビュー。
// Load/Reload はディスク上のJSONを読み直して snapshot を丸ごと差し替えるだけなので、
// 参照側(HTTPハンドラ)はロック不要で常に一貫したスナップショットを読める。
type Store struct {
	dataDir string
	current atomic.Pointer[snapshot]
}

// NewStore は dataDir を読み込み先とする Store を作る。呼び出し後すぐに Reload で初回ロードすること。
func NewStore(dataDir string) *Store {
	return &Store{dataDir: dataDir}
}

// Reload は dataDir 配下の banks.json / branches/*.json を読み直し、スナップショットを差し替える。
func (s *Store) Reload() error {
	snap, err := loadSnapshot(s.dataDir)
	if err != nil {
		return err
	}
	s.current.Store(snap)
	return nil
}

// Ready はまだ一度も Reload に成功していない(=データ未取得)かどうかを返す。
func (s *Store) Ready() bool {
	return s.current.Load() != nil
}

// UpdatedAt はロード済みデータの取得元更新日時（zengin-code/source-data の data/updated_at 由来）。
func (s *Store) UpdatedAt() string {
	snap := s.current.Load()
	if snap == nil {
		return ""
	}
	return snap.updatedAt
}

// ListBanks は全金融機関を code 昇順で返す。q を指定すると code/name/kana/hira/roma の部分一致で絞り込む。
func (s *Store) ListBanks(q string) []Bank {
	snap := s.current.Load()
	if snap == nil {
		return nil
	}
	if q == "" {
		return snap.banks
	}
	out := make([]Bank, 0)
	for _, b := range snap.banks {
		if bankMatches(b, q) {
			out = append(out, b)
		}
	}
	return out
}

// GetBank は code に一致する金融機関を返す。見つからなければ ok=false。
func (s *Store) GetBank(code string) (Bank, bool) {
	snap := s.current.Load()
	if snap == nil {
		return Bank{}, false
	}
	b, ok := snap.bankByCode[code]
	return b, ok
}

// ListBranches は bankCode の支店一覧を code 昇順で返す。q を指定すると部分一致で絞り込む。
// bankCode が空文字なら全金融機関の支店を返す。
func (s *Store) ListBranches(bankCode, q string) []Branch {
	snap := s.current.Load()
	if snap == nil {
		return nil
	}

	var src []Branch
	if bankCode == "" {
		src = make([]Branch, 0, len(snap.branchByKey))
		for _, bs := range snap.branchesByBank {
			src = append(src, bs...)
		}
		sort.Slice(src, func(i, j int) bool {
			if src[i].BankCode != src[j].BankCode {
				return src[i].BankCode < src[j].BankCode
			}
			return src[i].Code < src[j].Code
		})
	} else {
		src = snap.branchesByBank[bankCode]
	}

	if q == "" {
		return src
	}
	out := make([]Branch, 0)
	for _, br := range src {
		if branchMatches(br, q) {
			out = append(out, br)
		}
	}
	return out
}

// GetBranch は bankCode/code に一致する支店を返す。見つからなければ ok=false。
func (s *Store) GetBranch(bankCode, code string) (Branch, bool) {
	snap := s.current.Load()
	if snap == nil {
		return Branch{}, false
	}
	br, ok := snap.branchByKey[branchKey{bankCode: bankCode, code: code}]
	return br, ok
}

func bankMatches(b Bank, q string) bool {
	return strings.Contains(b.Code, q) ||
		strings.Contains(b.Name, q) ||
		strings.Contains(b.Kana, q) ||
		strings.Contains(b.Hira, q) ||
		strings.Contains(b.Roma, q)
}

func branchMatches(b Branch, q string) bool {
	return strings.Contains(b.Code, q) ||
		strings.Contains(b.Name, q) ||
		strings.Contains(b.Kana, q) ||
		strings.Contains(b.Hira, q) ||
		strings.Contains(b.Roma, q)
}

func loadSnapshot(dataDir string) (*snapshot, error) {
	banks, err := loadBanks(filepath.Join(dataDir, "banks.json"))
	if err != nil {
		return nil, fmt.Errorf("zengindata: banks.json 読み込みに失敗しました: %w", err)
	}

	branchesByBank, err := loadBranches(filepath.Join(dataDir, "branches"))
	if err != nil {
		return nil, fmt.Errorf("zengindata: branches 読み込みに失敗しました: %w", err)
	}

	updatedAt, err := os.ReadFile(filepath.Join(dataDir, "updated_at"))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("zengindata: updated_at 読み込みに失敗しました: %w", err)
	}

	snap := &snapshot{
		banks:          banks,
		bankByCode:     make(map[string]Bank, len(banks)),
		branchesByBank: branchesByBank,
		branchByKey:    make(map[branchKey]Branch),
		updatedAt:      strings.TrimSpace(string(updatedAt)),
	}
	for _, b := range banks {
		snap.bankByCode[b.Code] = b
	}
	for bankCode, branches := range branchesByBank {
		for _, br := range branches {
			snap.branchByKey[branchKey{bankCode: bankCode, code: br.Code}] = br
		}
	}

	return snap, nil
}

func loadBanks(path string) ([]Bank, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var banks []Bank
	if err := json.NewDecoder(f).Decode(&banks); err != nil {
		return nil, err
	}
	sort.Slice(banks, func(i, j int) bool { return banks[i].Code < banks[j].Code })
	return banks, nil
}

func loadBranches(dir string) (map[string][]Branch, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string][]Branch{}, nil
		}
		return nil, err
	}

	out := make(map[string][]Branch, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		bankCode := strings.TrimSuffix(entry.Name(), ".json")

		f, err := os.Open(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		var branches []Branch
		decErr := json.NewDecoder(f).Decode(&branches)
		f.Close()
		if decErr != nil {
			return nil, fmt.Errorf("%s: %w", entry.Name(), decErr)
		}

		sort.Slice(branches, func(i, j int) bool { return branches[i].Code < branches[j].Code })
		out[bankCode] = branches
	}
	return out, nil
}
