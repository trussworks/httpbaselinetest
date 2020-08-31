# httpbaselinetest

Think of your HTTP service as a state machine. A HTTP request is an
event that results in an HTTP response and a new state in your
database.

The httpbaselinetest package provides a framework for recording
requests, responses, and the expected database changes.

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
      -run TestBaselines/POST_v1_car_with_auth  -count=1

Now, let's look at the generated files

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
