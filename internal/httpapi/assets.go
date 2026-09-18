package httpapi

import "embed"

// assetsFS には /openapi.json・/docs・/ai 配下で配信する静的ドキュメントを埋め込む。
// 別ワークスペースのREADMEを読みに行かなくても、このAPI自身が実装に必要な情報
// (OpenAPI仕様・人間向けdocs・AIエージェント向けのconcepts/guides/examples)を返せるようにする。
//
//go:embed assets/openapi.json assets/docs.html assets/ai/*.json
var assetsFS embed.FS
