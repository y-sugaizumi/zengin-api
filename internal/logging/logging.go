// Package logging はアプリ共通の slog セットアップを提供する。
package logging

import (
	"log/slog"
	"os"
)

// New は本番運用を想定したJSON構造化ロガーを作る。
//
// ログファイルの管理・ローテーション・Lokiへの転送はホスト側のVectorが担うため、
// アプリはJSONをstdoutへ出力するだけでよい(growstar-docker-deploy skill 参照)。
func New(level slog.Level) *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	})
	return slog.New(handler)
}

// ParseLevel は "debug"/"info"/"warn"/"error" 等の文字列を slog.Level に変換する。
// 不明な値や空文字の場合は slog.LevelInfo を返す。
func ParseLevel(s string) slog.Level {
	var level slog.Level
	if err := level.UnmarshalText([]byte(s)); err != nil {
		return slog.LevelInfo
	}
	return level
}
