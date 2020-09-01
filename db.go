package httpbaselinetest

import (
	"bytes"
	"encoding/json"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/romanyx/polluter"
)

type formattedDbBaseline struct {
	NumRowsInserted uint64        `json:"numRowsInserted"`
	NumRowsUpdated  uint64        `json:"numRowsUpdated"`
	NumRowsDeleted  uint64        `json:"numRowsDeleted"`
	RemovedRows     []interface{} `json:"removedRows"`
	AddedRows       []interface{} `json:"addedRows"`
}

type pgStatUserTableInsUpdDel struct {
	Relname string `db:"relname"`
	NTupIns uint64 `db:"n_tup_ins"`
	NTupUpd uint64 `db:"n_tup_upd"`
	NTupDel uint64 `db:"n_tup_del"`
}

type dbTableInfo struct {
	InitialTableNames []string
	PgBaseline pgBaselineMap
}

type JsonTableData map[string]bool

type pgBaselineData struct {
	PgInsUpdDel     *pgStatUserTableInsUpdDel
	BeforeTableData *JsonTableData
}

type pgBaselineMap map[string]pgBaselineData

type dbBaseline map[string]formattedDbBaseline

func getTableStats(db *sqlx.DB, tableStats *[]pgStatUserTableInsUpdDel) error {
	// From https://www.postgresql.org/docs/current/monitoring-stats.html
	//
	// pg_stat_xact_all_tables
	//
	// Similar to pg_stat_all_tables, but counts actions taken so
	// far within the current transaction (which are not yet
	// included in pg_stat_all_tables and related views). The
	// columns for numbers of live and dead rows and vacuum and
	// analyze actions are not present in this view.
	sql := `
SELECT
  relname, n_tup_ins, n_tup_upd, n_tup_del
FROM
  pg_stat_xact_user_tables
`
	return db.Select(tableStats, sql)
}

func getJsonTableData(db *sqlx.DB, tableName string, jsonTableData *JsonTableData) error {
	sql := `SELECT to_jsonb("` + tableName + `".*) AS json_data FROM "` +
		tableName + `"`
	rows, err := db.Queryx(sql)
	if err != nil {
		return err
	}
	for rows.Next() {
		var jsonData string
		err = rows.Scan(&jsonData)
		if err != nil {
			return err
		}
		(*jsonTableData)[jsonData] = true
	}
	return nil
}

func buildFormattedDbBaseline(pgInsUpDel pgStatUserTableInsUpdDel,
	removedRows []string, addedRows []string) (formattedDbBaseline, error) {
	removedRowsJson := make([]interface{}, len(removedRows))
	addedRowsJson := make([]interface{}, len(addedRows))
	for i := range removedRows {
		var v interface{}
		err := json.Unmarshal([]byte(removedRows[i]), &v)
		if err != nil {
			return formattedDbBaseline{}, err
		}
		removedRowsJson[i] = v
	}
	for i := range addedRows {
		var v interface{}
		err := json.Unmarshal([]byte(addedRows[i]), &v)
		if err != nil {
			return formattedDbBaseline{}, err
		}
		addedRowsJson[i] = v
	}
	fdb := formattedDbBaseline{
		NumRowsInserted: pgInsUpDel.NTupIns,
		NumRowsUpdated:  pgInsUpDel.NTupUpd,
		NumRowsDeleted:  pgInsUpDel.NTupDel,
		RemovedRows:     removedRowsJson,
		AddedRows:       addedRowsJson,
	}
	return fdb, nil
}

func formatDb(fullDbBaseline map[string]formattedDbBaseline) ([]byte, error) {
	// use encoder to add trailing newline
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	err := enc.Encode(fullDbBaseline)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (r *httpBaselineTestRunner) seedWithPolluter() {
	polluterPath := path.Join(r.suite.baselineDir, r.btest.Seed)
	f, err := os.Open(polluterPath)
	if err != nil {
		r.t.Fatalf("Error opening seed file '%s': %s", polluterPath, err)
	}
	p := polluter.New(polluter.PostgresEngine(r.btest.Db.DB))
	defer f.Close()
	err = p.Pollute(f)
	if err != nil {
		r.t.Fatalf("Error polluting db: %s", err)
	}
}

func (r *httpBaselineTestRunner) getDbTableInfo() {
	beforeTableStats := []pgStatUserTableInsUpdDel{}
	err := getTableStats(r.btest.Db, &beforeTableStats)
	if err != nil {
		r.t.Fatalf("Error selecting user table info: %s", err)
	}
	r.dbTableInfo.InitialTableNames = make([]string, len(beforeTableStats))
	r.dbTableInfo.PgBaseline = make(pgBaselineMap, 0)
	jsonTableDataMap := make(map[string]*JsonTableData)
	for _, tableName := range r.btest.Tables {
		jtd := make(JsonTableData)
		err := getJsonTableData(r.btest.Db, tableName, &jtd)
		if err != nil {
			r.t.Fatalf("Error getting data for %s: %s", tableName, err)
		}
		jsonTableDataMap[tableName] = &jtd
	}
	for i := range beforeTableStats {
		tableName := beforeTableStats[i].Relname
		r.dbTableInfo.InitialTableNames[i] = tableName
		jtd := jsonTableDataMap[tableName]
		r.dbTableInfo.PgBaseline[tableName] = pgBaselineData{
			PgInsUpdDel:     &beforeTableStats[i],
			BeforeTableData: jtd,
		}
	}
	sort.Strings(r.dbTableInfo.InitialTableNames)
}

func (r *httpBaselineTestRunner) dbTestSetup() {
	if r.btest.Seed != "" {
		r.seedWithPolluter()
	}
	if r.btest.SeedFunc != nil {
		err := r.btest.SeedFunc(r.btest)
		if err != nil {
			r.t.Fatalf("Error running SeedFunc: %s", err)
		}
	}
	r.getDbTableInfo()
}

func (r *httpBaselineTestRunner) generateDbBaseline() dbBaseline {
	afterTableStats := []pgStatUserTableInsUpdDel{}
	err := getTableStats(r.btest.Db, &afterTableStats)
	if err != nil {
		r.t.Fatalf("Error selecting user table info: %s", err)
	}
	afterTableNames := make([]string, len(afterTableStats))
	afterTableMap := make(map[string]pgStatUserTableInsUpdDel)
	for i := range afterTableStats {
		tableName := afterTableStats[i].Relname
		afterTableNames[i] = tableName
		afterTableMap[tableName] = afterTableStats[i]
	}
	sort.Strings(afterTableNames)
	beforeKeyString := strings.Join(r.dbTableInfo.InitialTableNames, ",")
	afterKeyString := strings.Join(afterTableNames, ",")
	if beforeKeyString != afterKeyString {
		r.t.Errorf("Before tables: %s", beforeKeyString)
		r.t.Errorf("After tables: %s", afterKeyString)
		r.t.Fatal("Tables created/deleted in test")
	}

	fullDbBaseline := make(map[string]formattedDbBaseline, 0)
	for _, tableName := range r.dbTableInfo.InitialTableNames {
		beforePgInsUpdDel := *(r.dbTableInfo.PgBaseline[tableName].PgInsUpdDel)
		afterPgInsUpdDel := afterTableMap[tableName]
		diffPgInsUpdDel := pgStatUserTableInsUpdDel{
			Relname: tableName,
			NTupIns: afterPgInsUpdDel.NTupIns - beforePgInsUpdDel.NTupIns,
			NTupUpd: afterPgInsUpdDel.NTupDel - beforePgInsUpdDel.NTupDel,
			NTupDel: afterPgInsUpdDel.NTupDel - beforePgInsUpdDel.NTupDel,
		}
		if r.dbTableInfo.PgBaseline[tableName].BeforeTableData != nil {
			// BeforeTableData means this is in btest.Tables
			afterJsonTableData := make(JsonTableData)
			err := getJsonTableData(r.btest.Db, tableName, &afterJsonTableData)
			if err != nil {
				r.t.Fatalf("Error getting data for %s: %s", tableName, err)
			}
			removedRows := make([]string, 0)
			addedRows := make([]string, 0)
			btd := *(r.dbTableInfo.PgBaseline[tableName].BeforeTableData)
			for row := range btd {
				if !afterJsonTableData[row] {
					removedRows = append(removedRows, row)
				}
			}
			for row := range afterJsonTableData {
				if !btd[row] {
					addedRows = append(addedRows, row)
				}
			}
			tableDbBaseline, err := buildFormattedDbBaseline(diffPgInsUpdDel, removedRows, addedRows)
			fullDbBaseline[tableName] = tableDbBaseline
		} else {
			if beforePgInsUpdDel != afterPgInsUpdDel {
				fullDbBaseline[tableName] = formattedDbBaseline{
					NumRowsInserted: diffPgInsUpdDel.NTupIns,
					NumRowsDeleted: diffPgInsUpdDel.NTupDel,
					NumRowsUpdated: diffPgInsUpdDel.NTupUpd,
				}
			}
		}
	}

	return fullDbBaseline
}

func (r *httpBaselineTestRunner) assertNoDbChanges(fullDbBaseline map[string]formattedDbBaseline) {
	if (len(fullDbBaseline) != 0) {
		for tableName, tableDiff := range fullDbBaseline {
			r.t.Errorf("Unexpected table change for %s: %d row(s) inserted, %d row(s) updated, %d row(s) deleted",
				tableName, tableDiff.NumRowsInserted,
				tableDiff.NumRowsUpdated,
				tableDiff.NumRowsDeleted)
		}
		r.t.FailNow()
	}
}

