package main

import (
	"database/sql"
	"fmt"
	"os"

	_ "modernc.org/sqlite"
)

const dbPath = `C:/Users/Javier Css/.arn/scratch/ARN-1090/c3/c3.db`

var bodies = []string{
	"the runtime fell over",
	"a runtime boundary needs a seam",
	"runtime runtime runtime",
	"the port is still a port",
	"runtime is not a boundary",
}

func line(cmd string) {
	fmt.Println()
	fmt.Println("> " + cmd)
}

func exec(db *sql.DB, cmd string) error {
	line(cmd)
	_, err := db.Exec(cmd)
	if err != nil {
		fmt.Println("ERROR: " + err.Error())
		return err
	}
	return nil
}

func must(err error) {
	if err != nil {
		fmt.Println("FATAL: " + err.Error())
		os.Exit(1)
	}
}

func insertAll(db *sql.DB, table string) {
	for i, body := range bodies {
		cmd := fmt.Sprintf("INSERT INTO %s(rowid, body) VALUES (%d, '%s');", table, i+1, body)
		line(cmd)
		if _, err := db.Exec(cmd); err != nil {
			fmt.Println("ERROR: " + err.Error())
		}
	}
	line(fmt.Sprintf("SELECT count(*) FROM %s;", table))
	var n int
	must(db.QueryRow(fmt.Sprintf("SELECT count(*) FROM %s;", table)).Scan(&n))
	fmt.Printf("rows = %d\n", n)
}

func count(db *sql.DB, table, query string) int {
	cmd := fmt.Sprintf("SELECT count(*) FROM %s WHERE %s MATCH '%s';", table, table, query)
	line(cmd)
	var n int
	if err := db.QueryRow(cmd).Scan(&n); err != nil {
		fmt.Println("ERROR: " + err.Error())
		return -1
	}
	fmt.Printf("rows = %d\n", n)
	return n
}

func dumpScores(db *sql.DB) {
	cmd := "SELECT rowid, body, bm25(con_trigram) AS score FROM con_trigram WHERE con_trigram MATCH 'untime' ORDER BY score;"
	line(cmd)
	rows, err := db.Query(cmd)
	if err != nil {
		fmt.Println("ERROR: " + err.Error())
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		var body string
		var score float64
		must(rows.Scan(&id, &body, &score))
		fmt.Printf("rowid=%d score=%v body=%s\n", id, score, body)
	}
	if err := rows.Err(); err != nil {
		fmt.Println("ERROR: " + err.Error())
	}
}

func main() {
	fmt.Println("db_path = " + dbPath)
	_ = os.Remove(dbPath)
	defer os.Remove(dbPath)

	db, err := sql.Open("sqlite", dbPath)
	must(err)
	defer db.Close()
	must(db.Ping())

	line("SELECT sqlite_version();")
	var version string
	must(db.QueryRow("SELECT sqlite_version()").Scan(&version))
	fmt.Println(version)

	errTri := exec(db, "CREATE VIRTUAL TABLE con_trigram USING fts5(body, tokenize='trigram');")
	errDef := exec(db, "CREATE VIRTUAL TABLE con_default USING fts5(body);")

	if errTri == nil {
		insertAll(db, "con_trigram")
	}
	if errDef == nil {
		insertAll(db, "con_default")
	}

	if errTri != nil {
		fmt.Println("VERDICT trigram_tokenizer_unavailable")
		return
	}

	tri := count(db, "con_trigram", "untime")
	def := count(db, "con_default", "untime")
	line("SELECT 'difference', (SELECT count(*) FROM con_trigram WHERE con_trigram MATCH 'untime') - (SELECT count(*) FROM con_default WHERE con_default MATCH 'untime');")
	var diff int
	must(db.QueryRow("SELECT (SELECT count(*) FROM con_trigram WHERE con_trigram MATCH 'untime') - (SELECT count(*) FROM con_default WHERE con_default MATCH 'untime');").Scan(&diff))
	fmt.Println("rows = " + fmt.Sprint(diff))

	dumpScores(db)

	fmt.Println()
	fmt.Println("SUMMARY sqlite_version=" + version)
	fmt.Printf("SUMMARY ununtime_trigram_rows=%d\n", tri)
	fmt.Printf("SUMMARY ununtime_default_rows=%d\n", def)
	fmt.Printf("SUMMARY difference_rows=%d\n", diff)
}
