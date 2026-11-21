// Package copyhumanize rewrites machine-sounding Chinese copy into something a
// person would actually write.
//
// The rules here are deliberately mechanical and inspectable: each one is a
// named, reversible edit an editor could explain. Nothing in this package
// targets AI-detection scores — there is no attempt to game a classifier, only
// to remove the verbal habits that make generated copy read as filler.
//
// Rules load from user-editable JSON so the operator can disable a rule or add
// a term without a rebuild.
package copyhumanize

import "regexp"

// Rule is one rewrite. Patterns are applied in order; a rule never re-applies
// to its own output because each pass is a single replace over the input.
type Rule struct {
	// ID is stable and appears in the report, so an editor can cite it.
	ID string `json:"id"`
	// Label is the human name shown in the interface.
	Label string `json:"label"`
	// Why explains what reads as machine-written, in one line.
	Why string `json:"why"`
	// Pattern is a Go regular expression matched against the text.
	Pattern string `json:"pattern"`
	// Replace is the substitution; it may reference capture groups.
	Replace string `json:"replace"`
	// Enabled lets an operator switch a rule off without deleting it.
	Enabled bool `json:"enabled"`

	compiled *regexp.Regexp
}

// DefaultRules is the shipped rule pack.
//
// Each entry targets a specific, observable habit rather than a style opinion:
// empty openers, hedging, tricolon, buzzwords, and over-signposting. Wording
// that merely sounds formal (for example 用于) is left alone because it is often
// the clearest choice.
func DefaultRules() []Rule {
	return []Rule{
		{
			ID: "drop-era-opener", Label: "删「时代/浪潮」开场", Enabled: true,
			Why:     "以宏观时代开头的句子几乎不携带信息，读者会跳过。",
			Pattern: `(在当今|在这个|在当前)[^。！？\n]{0,12}(时代|浪潮|背景下)[，,]?`,
			Replace: "",
		},
		{
			ID: "drop-as-we-all-know", Label: "删「众所周知」", Enabled: true,
			Why:     "既然众所周知，就不必写；写了反而显得在凑字数。",
			Pattern: `(众所周知|毋庸置疑|不言而喻)[，,]?`,
			Replace: "",
		},
		{
			ID: "drop-hedge", Label: "去叠加对冲词", Enabled: true,
			Why:     "「可能大概也许」叠在一起会让结论听起来没有依据。",
			Pattern: `(可能|大概|也许|或许)(可能|大概|也许|或许)+`,
			Replace: "可能",
		},
		{
			ID: "drop-not-only-but-also", Label: "拆「不仅…而且…」", Enabled: true,
			Why:     "这组关联词是典型的作文腔，拆成短句更有力。",
			Pattern: `不仅([^，。！？\n]{1,30})[，,]\s*而且`,
			Replace: "$1，",
		},
		{
			ID: "drop-buzzword-enable", Label: "替换「赋能」", Enabled: true,
			Why:     "「赋能」不说明做了什么。",
			Pattern: `赋能`,
			Replace: "帮助",
		},
		{
			ID: "drop-buzzword-closed-loop", Label: "替换「闭环」", Enabled: true,
			Why:     "「闭环」是流程术语，读者更关心具体动作。",
			Pattern: `打造闭环|形成闭环|业务闭环`,
			Replace: "把事情做完并回看结果",
		},
		{
			ID: "drop-buzzword-grip", Label: "替换「抓手」", Enabled: true,
			Why:     "「抓手」让人不知道具体指什么。",
			Pattern: `抓手`,
			Replace: "切入点",
		},
		{
			ID: "drop-summary-opener", Label: "删「综上所述」", Enabled: true,
			Why:     "结论直接说，不需要预告。",
			Pattern: `(综上所述|总的来说|总而言之)[，,]?`,
			Replace: "",
		},
		{
			ID: "drop-firstly-order", Label: "删「首先/其次/最后」", Enabled: true,
			Why:     "顺序词堆叠会让段落像模板；改用分点或直接陈述。",
			Pattern: `(首先|其次|再次|最后)[，,]`,
			Replace: "",
		},
		{
			ID: "drop-emoji-run", Label: "收掉连续表情", Enabled: true,
			Why:     "一行堆三个以上表情会盖过正文。",
			Pattern: `([\x{1F300}-\x{1FAFF}\x{2600}-\x{27BF}]\s*){3,}`,
			Replace: "",
		},
		{
			ID: "fold-em-dash", Label: "破折号改逗号", Enabled: true,
			Why:     "中文正文里连续破折号读起来像翻译腔。",
			Pattern: `——`,
			Replace: "，",
		},
		{
			ID: "drop-very-very", Label: "去副词叠加", Enabled: true,
			Why:     "「非常非常」用加倍副词代替了具体程度。",
			Pattern: `(非常|特别|极其)(非常|特别|极其)+`,
			Replace: "$1",
		},
		{
			ID: "drop-question-opener", Label: "删问句开场", Enabled: true,
			Why: "「你是否也…？」是常见的空转开场，换成具体场景更有代入感。",
			// Only a question that opens the line is an opener. Allowing an
			// arbitrary prefix swallowed real lead-ins like "今天想聊聊：你是否…".
			Pattern: `^(你是否|你有没有|是不是也)[^\n]{0,30}？\s*\n?`,
			Replace: "",
		},
		{
			ID: "trim-leading-punct", Label: "清理行首标点", Enabled: true,
			Why: "删除整句后，原本在中间的逗号会留在行首。",
			// 冒号常是列表项的起点，只有这些句读符号才是删句残留。
			Pattern: `(?m)^[，,、；;]\s*`,
			Replace: "",
		},
		{
			ID: "collapse-space", Label: "合并多余空格", Enabled: true,
			Why:     "替换后容易留下双空格与行首空白。",
			Pattern: `[ \t]{2,}`,
			Replace: " ",
		},
		{
			ID: "trim-blank-lines", Label: "压缩连续空行", Enabled: true,
			Why:     "三行以上空行会把正文切碎。",
			Pattern: `\n{3,}`,
			Replace: "\n\n",
		},
	}
}
