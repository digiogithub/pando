package styles

import (
	"path/filepath"
	"strings"
)

const (
	PandoIcon    string = "木"
	OpenCodeIcon        = PandoIcon

	// Diagnostics
	CheckIcon   string = "󰗠"
	ErrorIcon   string = "󰅚"
	WarningIcon string = "󰀪"
	InfoIcon    string = "󰋽"
	HintIcon    string = "󰌵"

	// Status
	SpinnerIcon string = "󰔟"
	LoadingIcon string = "󰑓"
	SuccessIcon string = "󰄬"
	FailureIcon string = "󰜺"

	// Files & documents
	DocumentIcon   string = "󰈙"
	FileIcon       string = "󰈙"
	FolderIcon     string = "󰉋"
	FolderOpenIcon string = "󰝰"

	// Git
	GitBranchIcon   string = "󰘬"
	GitCommitIcon   string = "󰜘"
	GitMergeIcon    string = "󰘬"
	GitAddedIcon    string = "󰐕"
	GitRemovedIcon  string = "󰍴"
	GitModifiedIcon string = "󰏫"

	// Navigation
	ArrowRightIcon string = "󰁔"
	ArrowDownIcon  string = "󰁅"
	ChevronRight   string = "󰅂"
	ChevronDown    string = "󰅀"
	ChevronLeft    string = "󰅁"
	ChevronUp      string = "󰅃"

	// UI
	SearchIcon    string = "󰍉"
	SettingsIcon  string = "󰒓"
	ChatIcon      string = "󰭹"
	TerminalIcon  string = "󰆍"
	CloseIcon     string = "󰅖"
	PlusIcon      string = "󰐕"
	MinusIcon     string = "󰍴"
	LockIcon      string = "󰌾"
	UnlockIcon    string = "󰌿"
	ClipboardIcon string = "󰅌"
	BookmarkIcon  string = "󰃃"

	// Provider / AI
	RobotIcon string = "󰚩"
	AIIcon    string = "󱜚"
	BrainIcon string = "󰧠"
	MagicIcon string = "󰘦"

	// Misc
	BugIcon       string = "󰨰"
	FlameIcon     string = "󰈸"
	LightbulbIcon string = "󰌵"
	ClockIcon     string = "󰥔"
	CalendarIcon  string = "󰃭"
	TagIcon       string = "󰓹"
	LinkIcon      string = "󰌹"
	KeyIcon       string = "󰌋"
	ShieldIcon    string = "󰒃"
	DatabaseIcon  string = "󰆼"
	CloudIcon     string = "󰅟"
	PackageIcon   string = "󰏗"
)

var SpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

var fileNameIcons = map[string]string{
	"dockerfile":          "󰡨",
	"docker-compose.yml":  "󰡨",
	"docker-compose.yaml": "󰡨",
	"makefile":            "󱁤",
	"cmakelists.txt":      "󰔷",
	"license":             "󰿃",
	"readme":              "󰍔",
	"readme.md":           "󰍔",
	"readme.txt":          "󰍔",
	".gitignore":          "󰊢",
	".gitattributes":      "󰊢",
	".gitmodules":         "󰊢",
	".editorconfig":       "󰒓",
	".env":                "󰌾",
	".env.local":          "󰌾",
	".env.example":        "󰈞",
	"go.mod":              "󰟓",
	"go.sum":              "󰟓",
	"cargo.toml":          "󰣶",
	"cargo.lock":          "󰣶",
	"package.json":        "󰎙",
	"package-lock.json":   "󰎙",
	"tsconfig.json":       "󰛦",
	"webpack.config.js":   "󰜫",
	"vite.config.ts":      "󱐋",
	"vite.config.js":      "󱐋",
	".eslintrc":           "󰱺",
	".eslintrc.js":        "󰱺",
	".eslintrc.json":      "󰱺",
	".prettierrc":         "󰏫",
	"gemfile":             "󰴭",
	"rakefile":            "󰴭",
	"requirements.txt":    "󰌠",
	"setup.py":            "󰌠",
	"pyproject.toml":      "󰌠",
	"flake.nix":           "󱄅",
	"default.nix":         "󱄅",
	"shell.nix":           "󱄅",
	"mix.exs":             "󰣹",
	"mix.lock":            "󰣹",
}

var fileExtIcons = map[string]string{
	".go":         "󰟓",
	".mod":        "󰟓",
	".sum":        "󰟓",
	".py":         "󰌠",
	".pyi":        "",
	".pyc":        "",
	".rs":         "󱘗",
	".c":          "󰙱",
	".h":          "󰙱",
	".cpp":        "󰙲",
	".cxx":        "",
	".cc":         "",
	".hpp":        "",
	".hxx":        "",
	".java":       "󰬷",
	".jar":        "",
	".class":      "",
	".rb":         "󰴭",
	".erb":        "",
	".php":        "󰌟",
	".js":         "󰌞",
	".mjs":        "",
	".cjs":        "",
	".jsx":        "",
	".ts":         "󰛦",
	".tsx":        "󰜈",
	".html":       "󰌝",
	".htm":        "",
	".css":        "󰌜",
	".scss":       "󰌜",
	".sass":       "󰌜",
	".less":       "󰌜",
	".lua":        "󰢱",
	".vim":        "󰕷",
	".swift":      "󰛥",
	".kt":         "󱈙",
	".kts":        "",
	".dart":       "󰢵",
	".r":          "󰟔",
	".rmd":        "󰟔",
	".zig":        "",
	".nim":        "",
	".ex":         "󰣹",
	".exs":        "󰣹",
	".erl":        "󰟀",
	".hrl":        "",
	".hs":         "",
	".lhs":        "",
	".ml":         "",
	".mli":        "",
	".clj":        "",
	".cljs":       "",
	".cljc":       "",
	".scala":      "",
	".sbt":        "",
	".graphql":    "󰡷",
	".gql":        "󰡷",
	".proto":      "󰯂",
	".sql":        "󰆼",
	".sh":         "󰆍",
	".bash":       "󰆍",
	".zsh":        "󰆍",
	".fish":       "󰆍",
	".ps1":        "󰨊",
	".md":         "󰍔",
	".mdx":        "󰍔",
	".json":       "󰘦",
	".jsonc":      "󰘦",
	".yaml":       "󰈙",
	".yml":        "󰈙",
	".toml":       "󰈙",
	".ini":        "󰈙",
	".cfg":        "󰈙",
	".conf":       "󰈙",
	".xml":        "󰗀",
	".csv":        "󰈛",
	".tsv":        "󰈛",
	".txt":        "󰈙",
	".log":        "󰈙",
	".svg":        "󰜡",
	".png":        "󰋩",
	".jpg":        "󰋩",
	".jpeg":       "󰋩",
	".gif":        "󰋩",
	".bmp":        "󰋩",
	".ico":        "󰋩",
	".webp":       "󰋩",
	".wasm":       "󰘧",
	".lock":       "󰌾",
	".env":        "󰌾",
	".dockerfile": "󰡨",
	".tf":         "󱁢",
	".hcl":        "󱁢",
	".nix":        "󱄅",
	".vue":        "󰡄",
	".svelte":     "󰜈",
	".astro":      "󰑣",
	".sol":        "󰡪",
	".tex":        "󰙩",
	".bib":        "",
	".pdf":        "󰈦",
	".zip":        "󰗄",
	".tar":        "󰗄",
	".gz":         "󰗄",
	".7z":         "󰗄",
	".rar":        "󰗄",
	".rpm":        "󰗄",
	".deb":        "󰗄",
	".dmg":        "󰗄",
	".iso":        "󰗄",
}

func FileIconFor(name string) string {
	lower := strings.ToLower(filepath.Base(name))
	if icon, ok := fileNameIcons[lower]; ok {
		return icon
	}
	ext := strings.ToLower(filepath.Ext(name))
	if icon, ok := fileExtIcons[ext]; ok {
		return icon
	}
	return DocumentIcon
}
