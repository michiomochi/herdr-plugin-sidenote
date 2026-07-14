// Command sidenote は herdr の母艦 space 用のサイドノート TUI プラグイン。
//
//	sidenote watch   … TUI 常駐（各 space の状況を一覧表示）
//	sidenote set     … space の状況を全体設定（母艦が使う）
//	sidenote update  … space の状況を部分更新（母艦が使う）
//	sidenote clear   … space の記録を削除
//	sidenote list    … state をダンプ（デバッグ用）
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/michiomochi/herdr-plugin-sidenote/internal/cli"
	"github.com/michiomochi/herdr-plugin-sidenote/internal/state"
	"github.com/michiomochi/herdr-plugin-sidenote/internal/tui"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	var err error
	switch cmd {
	case "set":
		err = runSet(args)
	case "update":
		err = runUpdate(args)
	case "done":
		err = runDone(args)
	case "clear":
		err = runClear(args)
	case "list":
		err = runList(args)
	case "watch":
		err = runWatch(args)
	case "-h", "--help", "help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "不明なコマンド: %s\n\n", cmd)
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `sidenote - herdr 母艦 space 用サイドノート

使い方:
  sidenote watch  [--dir DIR] [--interval SEC]
  sidenote set    --space S [--workspace-id W] --headline H --status ST [オプション]
  sidenote update --key K [変更するフィールドのみ]
  sidenote done   --key K --text "完了項目"   （完了履歴に積む）
  sidenote clear  --key K
  sidenote list   [--dir DIR]

共通オプション:
  --dir DIR   state ディレクトリ (既定: $SIDENOTE_STATE_DIR または ~/.herdr/sidenote/state)

set/update のフィールドオプション:
  --space S --workspace-id W --headline H --status ST(planning/working/blocked/review/done)
  --next N --summary SUM --percent P(0-100)
  --step "label:state"(繰り返し可, state=todo/doing/done)
  --blocker B(繰り返し可) --note N(繰り返し可)

key は workspace-id（無ければ space）を指定する。
`)
}

// stringSlice は繰り返し指定できる文字列フラグ。
type stringSlice []string

func (s *stringSlice) String() string     { return strings.Join(*s, ",") }
func (s *stringSlice) Set(v string) error { *s = append(*s, v); return nil }

// fieldFlags は set/update 共通のフィールドフラグを保持する。
type fieldFlags struct {
	space       string
	workspaceID string
	headline    string
	status      string
	next        string
	summary     string
	percent     int
	steps       stringSlice
	blockers    stringSlice
	notes       stringSlice
}

func registerFieldFlags(fs *flag.FlagSet) *fieldFlags {
	f := &fieldFlags{}
	fs.StringVar(&f.space, "space", "", "space 名（人間可読ラベル）")
	fs.StringVar(&f.workspaceID, "workspace-id", "", "herdr workspace id")
	fs.StringVar(&f.headline, "headline", "", "今やっていること（一言）")
	fs.StringVar(&f.status, "status", "", "planning/working/blocked/review/done")
	fs.StringVar(&f.next, "next", "", "次のアクション")
	fs.StringVar(&f.summary, "summary", "", "進捗サマリ")
	fs.IntVar(&f.percent, "percent", 0, "進捗率 0-100")
	fs.Var(&f.steps, "step", `"label:state" 形式（繰り返し可）`)
	fs.Var(&f.blockers, "blocker", "ブロッカー（繰り返し可）")
	fs.Var(&f.notes, "note", "補足メモ（繰り返し可）")
	return f
}

// toOptions は「実際に渡されたフラグ」だけを Options のポインタに詰める。
func (f *fieldFlags) toOptions(fs *flag.FlagSet) (cli.Options, error) {
	present := map[string]bool{}
	fs.Visit(func(fl *flag.Flag) { present[fl.Name] = true })

	var o cli.Options
	if present["space"] {
		o.Space = &f.space
	}
	if present["workspace-id"] {
		o.WorkspaceID = &f.workspaceID
	}
	if present["headline"] {
		o.Headline = &f.headline
	}
	if present["status"] {
		o.Status = &f.status
	}
	if present["next"] {
		o.Next = &f.next
	}
	if present["summary"] {
		o.Summary = &f.summary
	}
	if present["percent"] {
		p := f.percent
		o.Percent = &p
	}
	if present["step"] {
		steps := make([]state.Step, 0, len(f.steps))
		for _, raw := range f.steps {
			st, err := cli.ParseStep(raw)
			if err != nil {
				return cli.Options{}, err
			}
			steps = append(steps, st)
		}
		o.Steps = &steps
	}
	if present["blocker"] {
		bl := []string(f.blockers)
		o.Blockers = &bl
	}
	if present["note"] {
		nt := []string(f.notes)
		o.Notes = &nt
	}
	return o, nil
}

func resolveDir(flagDir string) (string, error) {
	if flagDir != "" {
		return flagDir, nil
	}
	return state.ResolveDir()
}

func nowRFC3339() string {
	return time.Now().Format(time.RFC3339)
}

func runSet(args []string) error {
	fs := flag.NewFlagSet("set", flag.ExitOnError)
	dir := fs.String("dir", "", "state ディレクトリ")
	f := registerFieldFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	o, err := f.toOptions(fs)
	if err != nil {
		return err
	}
	d, err := resolveDir(*dir)
	if err != nil {
		return err
	}
	return cli.Set(d, o, nowRFC3339())
}

func runUpdate(args []string) error {
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	dir := fs.String("dir", "", "state ディレクトリ")
	key := fs.String("key", "", "更新対象キー（workspace-id か space）")
	f := registerFieldFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *key == "" {
		// key 未指定時は workspace-id / space を対象キーとして使う
		if f.workspaceID != "" {
			*key = f.workspaceID
		} else if f.space != "" {
			*key = f.space
		}
	}
	if *key == "" {
		return fmt.Errorf("--key（または --workspace-id/--space）で更新対象を指定してください")
	}
	o, err := f.toOptions(fs)
	if err != nil {
		return err
	}
	d, err := resolveDir(*dir)
	if err != nil {
		return err
	}
	return cli.Update(d, *key, o, nowRFC3339())
}

func runDone(args []string) error {
	fs := flag.NewFlagSet("done", flag.ExitOnError)
	dir := fs.String("dir", "", "state ディレクトリ")
	key := fs.String("key", "", "対象キー（workspace-id か space）")
	text := fs.String("text", "", "完了項目のテキスト")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *key == "" {
		return fmt.Errorf("--key で対象を指定してください")
	}
	if *text == "" {
		return fmt.Errorf("--text で完了項目を指定してください")
	}
	d, err := resolveDir(*dir)
	if err != nil {
		return err
	}
	return cli.Done(d, *key, *text, nowRFC3339())
}

func runClear(args []string) error {
	fs := flag.NewFlagSet("clear", flag.ExitOnError)
	dir := fs.String("dir", "", "state ディレクトリ")
	key := fs.String("key", "", "削除対象キー（workspace-id か space）")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *key == "" {
		return fmt.Errorf("--key で削除対象を指定してください")
	}
	d, err := resolveDir(*dir)
	if err != nil {
		return err
	}
	return cli.Clear(d, *key)
}

func runList(args []string) error {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	dir := fs.String("dir", "", "state ディレクトリ")
	if err := fs.Parse(args); err != nil {
		return err
	}
	d, err := resolveDir(*dir)
	if err != nil {
		return err
	}
	return cli.List(d, os.Stdout)
}

func runWatch(args []string) error {
	fs := flag.NewFlagSet("watch", flag.ExitOnError)
	dir := fs.String("dir", "", "state ディレクトリ")
	interval := fs.Int("interval", 3, "herdr ポーリング間隔（秒）")
	if err := fs.Parse(args); err != nil {
		return err
	}
	d, err := resolveDir(*dir)
	if err != nil {
		return err
	}
	if err := state.EnsureDir(d); err != nil {
		return err
	}
	return tui.Run(d, time.Duration(*interval)*time.Second)
}
