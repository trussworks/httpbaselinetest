# httpbaselinetest

Think of your HTTP service as a state machine. A HTTP request is an
event that results in an HTTP response and a new state in your
database.

The httpbaselinetest package provides a framework for recording
requests, responses, and the expected database changes.

Currently only works with postgresql.

## Example

```go

setupFunc := func(name string, btest *baselinetest.BaselineTest) error {
  // Create your http handler here
  myserver := myhttp.NewServer()
  btest.Handler = myserver
  // Tell httpbaselinetest to use the db connection.
  // More on this feature later
  btest.Db = myserver.Db() 
  return nil
}
teardownFunc := func(t *testing.T, btest *baselinetest.BaselineTest) error {
  // Maybe clean something up?
  return nil
}
bts := httpbaselinetest.NewDefaultBaselineTestSuite(t)
bts.Run("POST v1 car with auth", httpbaselinetest.BaselineTest{
    Setup: setupFunc,
    Teardown: teardownFunc,
    Method:  http.MethodPost,
    Path:    "/api/v1/car",
    Body: map[string]string{
        "make":      "Honda",
        "model":     "Accord",
        "modelYear": "2020",
        "color":     "red",
    },
    Headers: map[string]string{
        "Authorization": "MySecret",
        "Content-Type": "application/json",
    },
    Tables: []string{"cars"},
})
```

First, generate a set of baselines

    $ REBASELINE=1 go test ./pkg/... \
      -run TestBaselines/POST_v1_car_with_auth -count=1
      
Now, run your baseline tests to make sure nothing has changed

    $ go test ./pkg/... \
      -run TestBaselines/POST_v1_car_with_auth -count=1
      
What happens if your baseline doesn't match?  Here the request has been changed:

    $ go test ./pkg/... \
      -run TestBaselines/POST_v1_car_with_auth -count=1
    --- FAIL: TestBaselines (0.01s)
    --- FAIL: TestBaselines/POST_v1_car_with_auth (0.01s)
        suite.go:222: Request Difference
        suite.go:223: 
            --- testdata/post_v1_car_with_auth.resp.txt (expected)
            +++ actual
            @@ -5 +5 @@
            -  "modelYear": "2020",
            +  "modelYear": "1999",
            @@ -11 +10,0 @@
            -


Let's look at the generated files from when `REBASELINE` was configured.

### Request File
```
# .../mytestpkg/testdata/post_v1_car_with_auth.req.txt
POST /api/v1/car HTTP/1.1
Host: example.com
Authorization: MySecret
Content-Type: application/json
Content-Length: 83

{
  "color": "red",
  "make": "Honda",
  "model": "Accord",
  "modelYear": "2020"
}
```

### Response File
```
# .../mytestpkg/testdata/post_v1_car_with_auth.resp.txt
HTTP/1.1 200 OK
Content-Type: application/json

{
  "color": "red",
  "id": "8fd7f84c-ce1c-463c-ba3f-ea81725f1eb4",
  "make": "Honda",
  "model": "Accord",
  "modelYear": "2020",
  "owner_id": "7f2272e3-f287-49d8-a384-4ca18b84c98f"
}
```

### DB File
```
# .../mytestpkg/testdata/post_v1_car_with_auth.db.json
{
  "cars": {
    "numRowsInserted": 1,
    "numRowsUpdated": 0,
    "numRowsDeleted": 0,
    "removedRows": [],
    "addedRows": [
      {
        "color": "red",
        "id": "8fd7f84c-ce1c-463c-ba3f-ea81725f1eb4",
        "make": "Honda",
        "model": "Accord",
        "modelYear": "2020",
        "owner_id": "7f2272e3-f287-49d8-a384-4ca18b84c98f"
      }
    ]
  }
}
```
## Database Baselines
The database baseline feature expects to be run inside a transaction.
It then uses
[pg_stat_xact_user_tables](https://www.postgresql.org/docs/current/monitoring-stats.html)
to track which tables have changes.  For tests that do expect database
changes, the `HttpBaselineTest.Tables` field should be set so that a
baseline of expected database changes can be created.  If your
baseline test makes changes to a table that is not configured, the
test will fail. 

### Testing with Transactions
Use [go-txdb](https://github.com/DATA-DOG/go-txdb) to have all of your
baseline tests run in a separate transaction so that any changes are
discarded at the end of the test.

You can create a separate fake database name for each test to ensure a
new connection is created.

### go-txdb basic example
```go
import (
	"github.com/DATA-DOG/go-txdb"
)
// ...

realDbUrl := "postgres://user:pass@dbhost:5432/my_test_db?sslmode=disable"
txdb.Register("pgx", "postgres", realDbUrl)
setupFunc := func(testName string, btest *httpbaselinetest.HttpBaselineTest) error {
  normalizedTestName := httpbaselinetest.NormalizeTestName(testName)
  testDbUrl := "pgx://user:pass@" + "txdb_" + normalizedTestName +
    " :5432/?sslmode=disable"
  server := myhttpserverpkg.NewServer(testDbUrl)
  btest.Handler = server
  btest.db = server.Db()
}
```

### go-txdb Pop example
[pop](https://github.com/gobuffalo/pop) makes this a bit more
exciting. This example is for v4.13.1 where you have to create your
own [pop.store](https://github.com/gobuffalo/pop/blob/v4.13.1/store.go).

```go
type BaselinePopStore struct {
	*sqlx.DB
}

func (bps *BaselinePopStore) Commit() error {
	return nil
}

func (bps *BaselinePopStore) Rollback() error {
	return nil
}

func (bps *BaselinePopStore) Transaction() (*pop.Tx, error) {
	t := &pop.Tx{
		ID: rand.Int(),
	}
	tx, err := bps.DB.Beginx()
	t.Tx = tx
	return t, fmt.Errorf("could not create new transaction %w", err)
}

func getPopConnectionDetails() pop.ConnectionDetails {
  return pop.ConnectionDetails {
    Dialect: "postgres",
    Database: "my_test_db",
    // ...
  }
}

func getDbUrl(popDetails pop.ConnectionDetails) string {
	return fmt.Sprintf("%s://%s:%s@%s:%s/%s?sslmode=%s",
		popDetails.Dialect, popDetails.User, popDetails.Password, popDetails.Host,
		popDetails.Port, popDetails.Database, popDetails.Options["sslmode"])
}

popDetails := getPopConnectionDetails()
realDbUrl := getDbUrl(popDetails)
txdb.Register("pgx", "postgres", realDbUrl)
popDetails.Driver = "pgx"
popDetails.Dialect = "postgres"
setupFunc := func(testName string, btest*httpbaselinetest.HttpBaselineTest) error {
  popDetails.Database = "txdb_" + httpbaselinetest.NormalizeTestName(name)
  popConn, err := pop.NewConnection(popDetails)
  if err != nil {
	return fmt.Errorf("Cannot create new POP connection: %s", err)
  }
  testDbUrl := getDbUrl(popDetails)
  db, err := sqlx.Connect(popDetails.Driver, testDbUrl)
  if err != nil {
	return err
  }
  popConn.Store = &BaselinePopStore{DB: db}
  // can now use popConn as usual
}
```

## Caveats and Complications
Thinking of your HTTP service as a state machine is very powerful, but
also may require re-thinking how you configure your service.  Ideally
you want a way to configure all of the initial state of your service.
That includes things like ways to [configure the current
time](https://godoc.org/github.com/facebookgo/clock), [generate UUIDs
from a specific random
source](https://github.com/google/uuid/blob/master/version4.go#L34),
etc.  If this is possible, then you can run your tests in parallel and
know that they will produce deterministic results.

Unfortunately, it's very common for go libraries to keep package
private global state.  You may have to find creative ways to reset
this state between tests.  This also means you cannot run your tests
in parallel.

Making your service deterministic is a challenge, but doing so can
help fully understand all of the state your service depends on.  If
you can capture this, that makes it more likely you can reproduce bugs
seen in production in a development environment.

Another consequence is that baselines need to be normalized. For
example, the order of fields in a JSON response shouldn't matter.
However, sometimes the order of objects in an array doesn't matter (it
can be whatever order is returned by the db), but sometimes it does
because the underlying request expects objects ordered by some
criteria.

Additional features to post-process baselines may be needed.

