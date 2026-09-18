// Package zengindata は全銀フォーマット（金融機関・支店マスタ）のデータ取得と保持を担う。
package zengindata

// Bank は金融機関マスタの1レコード。
type Bank struct {
	Code string `json:"code"`
	Name string `json:"name"`
	Kana string `json:"kana"`
	Hira string `json:"hira"`
	Roma string `json:"roma"`
}

// Branch は支店マスタの1レコード。
type Branch struct {
	BankCode string `json:"bank_code"`
	Code     string `json:"code"`
	Name     string `json:"name"`
	Kana     string `json:"kana"`
	Hira     string `json:"hira"`
	Roma     string `json:"roma"`
}
