package httpbaselinetest

import (
	"bytes"
	"encoding/json"
	"os"
	"sort"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/romanyx/polluter"

	"gopkg.in/yaml.v2" // same yaml used by polluter
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
	PgBaseline        pgBaselineMap
}

type JSONTableData map[string]bool

type pgBaselineData struct {
	PgInsUpdDel     *pgStatUserTableInsUpdDel
	BeforeTableData *JSONTableData
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
	//
	// We use pg_stat_xact_user_tables since that's all we are
	// interested in
	sql := `
SELECT
  relname, n_tup_ins, n_tup_upd, n_tup_del
FROM
  pg_stat_xact_user_tables
`
	return db.Select(tableStats, sql)
}

func getTableDependencyOrder(db *sqlx.DB) ([]string, error) {
	sql := `
SELECT kcu.table_name AS foreign_table,
       rel_tco.table_name AS primary_table
FROM information_schema.table_constraints tco
JOIN information_schema.key_column_usage kcu
          ON tco.constraint_schema = kcu.constraint_schema
          AND tco.constraint_name = kcu.constraint_name
JOIN information_schema.referential_constraints rco
          ON tco.constraint_schema = rco.constraint_schema
          AND tco.constraint_name = rco.constraint_name
JOIN information_schema.table_constraints rel_tco
          ON rco.unique_constraint_schema = rel_tco.constraint_schema
          AND rco.unique_constraint_name = rel_tco.constraint_name
WHERE tco.constraint_type = 'FOREIGN KEY'
GROUP BY kcu.table_name,
         rel_tco.table_name
ORDER BY kcu.table_name
`
	rows, err := db.Queryx(sql)
	if err != nil {
		return nil, err
	}
	depMap := make(map[string][]string)
	for rows.Next() {
		cols, err := rows.SliceScan()
		if err != nil {
			return nil, err
		}
		foreignTable := string(cols[0].([]uint8))
		primaryTable := string(cols[1].([]uint8))
		if foreignTable == primaryTable {
			continue
		}
		_, ok := depMap[foreignTable]
		if !ok {
			depMap[foreignTable] = make([]string, 0)
		}
		depMap[foreignTable] = append(depMap[foreignTable], primaryTable)
	}
	return dependencyOrder(depMap), nil
}

func dependencyOrder(depMap map[string][]string) []string {
	visited := make(map[string]bool)
	deps := []string{}
	todo := []string{}
	var tableName string
	// first put all tables into todo
	for tableName := range depMap {
		todo = append(todo, tableName)
	}
	for len(todo) > 0 {
		// pop the first entry
		tableName, todo = todo[0], todo[1:]
		// if already processed, skip
		if visited[tableName] {
			continue
		}
		tdeps, ok := depMap[tableName]
		// if this table has no dependencies, all done
		if !ok || len(tdeps) == 0 {
			deps = append(deps, tableName)
			visited[tableName] = true
			continue
		}
		// if all dependencies are done, append to deps
		allDepsVisited := true
		for _, depTableName := range tdeps {
			allDepsVisited = allDepsVisited && visited[depTableName]
		}
		if allDepsVisited {
			deps = append(deps, tableName)
			visited[tableName] = true
		} else {
			// prepend dependencies so they are processed
			// next
			tdeps = append(tdeps, tableName)
			todo = append(tdeps, todo...)
		}
	}
	return deps
}

func getJSONTableData(db *sqlx.DB, tableName string, jsonTableData *JSONTableData) error {
	sql := `SELECT to_jsonb("` + tableName + `".*) AS json_data FROM "` +
		tableName + `" ORDER BY 1`
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
	removedRowsJSON := make([]interface{}, len(removedRows))
	addedRowsJSON := make([]interface{}, len(addedRows))
	for i := range removedRows {
		var v interface{}
		err := json.Unmarshal([]byte(removedRows[i]), &v)
		if err != nil {
			return formattedDbBaseline{}, err
		}
		removedRowsJSON[i] = v
	}
	for i := range addedRows {
		var v interface{}
		err := json.Unmarshal([]byte(addedRows[i]), &v)
		if err != nil {
			return formattedDbBaseline{}, err
		}
		addedRowsJSON[i] = v
	}
	fdb := formattedDbBaseline{
		NumRowsInserted: pgInsUpDel.NTupIns,
		NumRowsUpdated:  pgInsUpDel.NTupUpd,
		NumRowsDeleted:  pgInsUpDel.NTupDel,
		RemovedRows:     removedRowsJSON,
		AddedRows:       addedRowsJSON,
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
	f, err := os.Open(r.seedPath)
	if err != nil {
		r.t.Fatalf("Error opening seed file '%s': %s", r.seedPath, err)
	}
	defer f.Close()
	p := polluter.New(polluter.PostgresEngine(r.btest.Db.DB))
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
	r.dbTableInfo.PgBaseline = make(pgBaselineMap)
	jsonTableDataMap := make(map[string]*JSONTableData)
	for _, tableName := range r.btest.Tables {
		jtd := make(JSONTableData)
		err := getJSONTableData(r.btest.Db, tableName, &jtd)
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

func doRegenerateSeed() bool {
	return os.Getenv("REGENERATE_SEED") != ""
}

func (r *httpBaselineTestRunner) dbTestSetup() {
	if doRegenerateSeed() {
		// getDbTableInfo only dumps rows for the test tables,
		// so fake that out by putting all tables in there temporarily
		origTestTables := r.btest.Tables
		allTableStats := []pgStatUserTableInsUpdDel{}
		err := getTableStats(r.btest.Db, &allTableStats)
		if err != nil {
			r.t.Fatalf("Error selecting user table info: %s", err)
		}
		allTables := make([]string, len(allTableStats))
		for i := range allTableStats {
			allTables[i] = allTableStats[i].Relname
		}
		r.btest.Tables = allTables
		r.getDbTableInfo()
		r.btest.Tables = origTestTables
	}
	if r.btest.SeedFunc != nil {
		err := r.btest.SeedFunc(r.btest)
		if err != nil {
			r.t.Fatalf("Error running SeedFunc: %s", err)
		}
		if doRegenerateSeed() {
			r.dumpForPolluter()
		}
	}
	if r.btest.Seed != "" && !doRegenerateSeed() {
		r.seedWithPolluter()
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

	fullDbBaseline := make(map[string]formattedDbBaseline)
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
			afterJSONTableData := make(JSONTableData)
			err := getJSONTableData(r.btest.Db, tableName, &afterJSONTableData)
			if err != nil {
				r.t.Fatalf("Error getting data for %s: %s", tableName, err)
			}
			removedRows := []string{}
			addedRows := []string{}
			btd := *(r.dbTableInfo.PgBaseline[tableName].BeforeTableData)
			for row := range btd {
				if !afterJSONTableData[row] {
					removedRows = append(removedRows, row)
				}
			}
			for row := range afterJSONTableData {
				if !btd[row] {
					addedRows = append(addedRows, row)
				}
			}
			tableDbBaseline, err := buildFormattedDbBaseline(diffPgInsUpdDel, removedRows, addedRows)
			if err != nil {
				r.t.Fatalf("Error building formatted db baseline %s", err)
			}
			fullDbBaseline[tableName] = tableDbBaseline
		} else {
			if beforePgInsUpdDel != afterPgInsUpdDel {
				fullDbBaseline[tableName] = formattedDbBaseline{
					NumRowsInserted: diffPgInsUpdDel.NTupIns,
					NumRowsDeleted:  diffPgInsUpdDel.NTupDel,
					NumRowsUpdated:  diffPgInsUpdDel.NTupUpd,
				}
			}
		}
	}

	return fullDbBaseline
}

func (r *httpBaselineTestRunner) assertNoDbChanges(fullDbBaseline map[string]formattedDbBaseline) {
	if len(fullDbBaseline) != 0 {
		for tableName, tableDiff := range fullDbBaseline {
			r.t.Errorf("Unexpected table change for %s: %d row(s) inserted, %d row(s) updated, %d row(s) deleted",
				tableName, tableDiff.NumRowsInserted,
				tableDiff.NumRowsUpdated,
				tableDiff.NumRowsDeleted)
		}
		r.t.FailNow()
	}
}

func (r *httpBaselineTestRunner) dumpForPolluter() {
	dbBaseline := r.generateDbBaseline()
	polluterMap := make(map[string][]interface{})
	for tableName := range dbBaseline {
		if len(dbBaseline[tableName].AddedRows) > 0 {
			polluterMap[tableName] = dbBaseline[tableName].AddedRows
		}
	}
	tableDeps, err := getTableDependencyOrder(r.btest.Db)
	if err != nil {
		r.t.Fatalf("Error getting table dependency order: %s", err)
	}
	yamlBytes := make([]byte, 0)
	for _, tableName := range tableDeps {
		tableData, ok := polluterMap[tableName]
		tableMap := make(map[string][]interface{})
		tableMap[tableName] = tableData
		if ok {
			bytes, err := yaml.Marshal(tableMap)
			if err != nil {
				r.t.Fatalf("Error regenerating seed yaml: %s", err)
			}
			yamlBytes = append(yamlBytes, bytes...)
		}
	}
	r.writeFile(r.seedPath, yamlBytes)

	// reset
	r.dbTableInfo = &dbTableInfo{}
}
