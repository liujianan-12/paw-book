// audit 只读执行业务一致性检查集（skills/slot-rule-guard/checks.sql），输出固定 JSON。
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type checkResult struct {
	ID     string   `json:"id"`
	Pass   bool     `json:"pass"`
	Rows   int      `json:"rows"`
	Sample []string `json:"sample,omitempty"`
}

type report struct {
	Skill    string        `json:"skill"`
	Version  string        `json:"version"`
	TS       string        `json:"ts"`
	Date     string        `json:"date"`
	StoreID  int64         `json:"storeId"`
	Commit   string        `json:"commit"`
	Checks   []checkResult `json:"checks"`
	Pass     bool          `json:"pass"`
	Commands []string      `json:"commands"`
}

func main() {
	dbPath := flag.String("db", "paw-book.db", "SQLite 文件路径（只读）")
	checksPath := flag.String("checks", "skills/slot-rule-guard/checks.sql", "检查 SQL 文件")
	storeID := flag.Int64("store", 1, "门店 ID")
	date := flag.String("date", "", "营业日 YYYY-MM-DD（必填）")
	commit := flag.String("commit", "", "被测提交 SHA")
	out := flag.String("out", "", "输出 JSON 路径；留空打印到 stdout")
	flag.Parse()
	if *date == "" {
		fmt.Fprintln(os.Stderr, "必须提供 -date YYYY-MM-DD")
		os.Exit(2)
	}
	if _, err := time.Parse("2006-01-02", *date); err != nil {
		fmt.Fprintf(os.Stderr, "date 非法: %v\n", err)
		os.Exit(2)
	}

	raw, err := os.ReadFile(*checksPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "读取检查集失败: %v\n", err)
		os.Exit(2)
	}
	stmts := splitStatements(string(raw))
	if len(stmts) == 0 {
		fmt.Fprintln(os.Stderr, "检查集为空")
		os.Exit(2)
	}

	dsn := fmt.Sprintf("file:%s?mode=ro&_pragma=busy_timeout(5000)", *dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "打开数据库失败: %v\n", err)
		os.Exit(2)
	}
	defer db.Close()
	ctx := context.Background()

	rep := report{Skill: "slot-rule-guard", Version: "0.1.0", TS: time.Now().Format(time.RFC3339),
		Date: *date, StoreID: *storeID, Commit: *commit, Pass: true}
	rep.Commands = []string{fmt.Sprintf("go run ./cmd/audit -db %s -date %s -store %d", *dbPath, *date, *storeID)}

	for _, s := range stmts {
		q := strings.ReplaceAll(s.sql, ":store", fmt.Sprint(*storeID))
		q = strings.ReplaceAll(q, ":date", "'"+*date+"'")
		rows, err := db.QueryContext(ctx, q)
		if err != nil {
			fmt.Fprintf(os.Stderr, "执行 %s 失败: %v\n", s.id, err)
			os.Exit(2)
		}
		cols, _ := rows.Columns()
		count := 0
		sample := []string{}
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				rows.Close()
				fmt.Fprintf(os.Stderr, "扫描 %s 失败: %v\n", s.id, err)
				os.Exit(2)
			}
			count++
			if len(sample) < 3 {
				sample = append(sample, fmt.Sprint(vals...))
			}
		}
		rows.Close()
		res := checkResult{ID: s.id, Pass: count == 0, Rows: count, Sample: sample}
		if !res.Pass {
			rep.Pass = false
		}
		rep.Checks = append(rep.Checks, res)
	}

	data, _ := json.MarshalIndent(rep, "", "  ")
	if *out != "" {
		if err := os.WriteFile(*out, data, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "写输出失败: %v\n", err)
			os.Exit(2)
		}
	}
	fmt.Println(string(data))
	if !rep.Pass {
		os.Exit(1)
	}
}

type statement struct {
	id  string
	sql string
}

// splitStatements 按 “-- check: <ID>” 分段，忽略空行与注释。
func splitStatements(text string) []statement {
	var out []statement
	id := ""
	var cur strings.Builder
	flush := func() {
		body := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(cur.String()), ";"))
		if body != "" {
			if id == "" {
				id = "UNKNOWN"
			}
			out = append(out, statement{id: id, sql: body})
		}
		cur.Reset()
	}
	for _, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "-- check:") {
			flush()
			id = strings.Fields(strings.TrimSpace(strings.TrimPrefix(t, "-- check:")))[0]
			continue
		}
		if t == "" || strings.HasPrefix(t, "--") {
			continue
		}
		cur.WriteString(line)
		cur.WriteString("\n")
		if strings.HasSuffix(t, ";") {
			flush()
			id = ""
		}
	}
	flush()
	return out
}
