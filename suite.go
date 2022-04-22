package httpbaselinetest

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/pmezard/go-difflib/difflib"
)

type Suite struct {
	t           *testing.T
	baselineDir string
}

func NewDefaultSuite(t *testing.T) *Suite {
	return &Suite{
		t:           t,
		baselineDir: "testdata",
	}
}

type SetupFunc func(testName string, baselineTest *HTTPBaselineTest) error
type TeardownFunc func(t *testing.T, baselineTest *HTTPBaselineTest) error
type SeedFunc func(baselineTest *HTTPBaselineTest) error
type BodyValidatorFunc func(body []byte) error
type HTTPBaselineTest struct {
	Setup    SetupFunc
	Teardown TeardownFunc
	Custom   interface{}

	Handler           http.Handler
	Method            string
	Path              string
	Host              string
	Body              interface{} // io.Reader or string
	Headers           map[string]string
	Cookies           []http.Cookie
	RequestValidator  BodyValidatorFunc
	ResponseValidator BodyValidatorFunc

	Db       *sqlx.DB
	Seed     string
	SeedFunc SeedFunc
	Tables   []string
}

type httpBaselineTestRunner struct {
	testName         string
	suite            *Suite
	btest            *HTTPBaselineTest
	t                *testing.T
	baselineReqPath  string
	baselineRespPath string
	baselineDbPath   string
	seedPath         string
	dbTableInfo      *dbTableInfo
}

func newRunner(testName string, t *testing.T, suite *Suite,
	btest *HTTPBaselineTest) httpBaselineTestRunner {
	if btest.Setup != nil {
		err := btest.Setup(testName, btest)
		if err != nil {
			t.Fatalf("Setup failed: %s", err)
		}
	}
	if btest.Handler == nil {
		t.Fatal("Handler is nil")
	}
	if btest.Method == "" {
		t.Fatal("Method is not provided")
	}
	if btest.Path == "" {
		t.Fatal("Path is not provided")
	}
	nPathPrefix := path.Join(suite.baselineDir, NormalizeTestName(testName))

	var seedPath string
	if btest.Seed != "" {
		seedPath = path.Join(suite.baselineDir, btest.Seed)
	}
	return httpBaselineTestRunner{
		testName:         testName,
		suite:            suite,
		btest:            btest,
		t:                t,
		baselineReqPath:  nPathPrefix + ".req.txt",
		baselineRespPath: nPathPrefix + ".resp.txt",
		baselineDbPath:   nPathPrefix + ".db.json",
		seedPath:         seedPath,
		dbTableInfo:      &dbTableInfo{},
	}
}

var disallowedTestChars = regexp.MustCompile(`[^[:word:]]`)

func NormalizeTestName(name string) string {
	tname := strings.ToLower(name)
	tbytes := disallowedTestChars.ReplaceAll([]byte(tname), []byte("_"))
	return string(tbytes)
}

func formatJSON(body []byte) (string, error) {
	var v interface{}
	err := json.Unmarshal(body, &v)
	if err != nil {
		return "", err
	}
	// use encoder to add trailing newline
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	err = enc.Encode(v)
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

func formatBody(contentType string, body []byte) (string, error) {
	if contentType == "application/json" {
		return formatJSON(body)
	}
	return string(body), nil

}

func doRebaseline() bool {
	return os.Getenv("REBASELINE") != ""
}

func (r *httpBaselineTestRunner) assertBaselineEquality(expectedPath string, formatted string) {
	expected, err := ioutil.ReadFile(expectedPath)
	if err != nil {
		r.t.Fatalf("Error reading baseline %s: %s", expectedPath, err)
	}
	diff := difflib.UnifiedDiff{
		A:        difflib.SplitLines(string(expected)),
		B:        difflib.SplitLines(formatted),
		FromFile: expectedPath + " (expected)",
		ToFile:   "actual",
		Eol:      "\n",
	}
	diffstr, err := difflib.GetUnifiedDiffString(diff)
	if err != nil {
		r.t.Fatalf("Error generating diff: %s", err)
	}
	if diffstr != "" {
		r.t.Error("Request Difference")
		r.t.Log("\n" + diffstr)
	}
}

func (r *httpBaselineTestRunner) writeFile(path string, formattedData []byte) {
	dir := filepath.Dir(path)
	err := os.MkdirAll(dir, 0755)
	if err != nil {
		r.t.Fatalf("Error creating dir %s: %s", dir, err)
	}
	// #nosec G306 -- The test files should be world readable
	err = ioutil.WriteFile(path, []byte(formattedData), 0644)
	if err != nil {
		r.t.Fatalf("Error writing baseline %s: %s", path, err)
	}
}

func (suite *Suite) Run(name string, btest HTTPBaselineTest) {
	suite.t.Run(name, func(t *testing.T) {
		// Should probably have some way of indicating if the
		// test should be t.Parallel()
		runner := newRunner(name, t, suite, &btest)

		if btest.Db != nil {
			runner.dbTestSetup()
			// make sure we close the db connection after
			// the test
			defer btest.Db.Close()
		}

		req := runner.buildRequest()
		formattedReq, rawReqBody, err := formatRequest(req)
		if err != nil {
			t.Fatalf("Error formatting request: %s", err)
		}
		if btest.RequestValidator != nil {
			err = btest.RequestValidator(rawReqBody)
			if err != nil {
				t.Errorf("Error validating request: %s", err)
			}
		}
		if doRebaseline() {
			runner.writeFile(runner.baselineReqPath,
				[]byte(formattedReq))
		} else {
			runner.assertBaselineEquality(runner.baselineReqPath,
				formattedReq)
		}

		recorder := httptest.NewRecorder()
		btest.Handler.ServeHTTP(recorder, req)
		formattedResp, rawRespBody, err := formatResponse(recorder.Result())
		if err != nil {
			t.Fatalf("Error formatting response: %s", err)
		}
		if btest.ResponseValidator != nil {
			err := btest.ResponseValidator(rawRespBody)
			if err != nil {
				t.Errorf("Error validating response: %s", err)
			}
		}
		if doRebaseline() {
			runner.writeFile(runner.baselineRespPath,
				[]byte(formattedResp))
		} else {
			runner.assertBaselineEquality(runner.baselineRespPath,
				formattedResp)
		}

		if btest.Db != nil {
			fullDbBaseline := runner.generateDbBaseline()
			if btest.Tables != nil {
				formattedDb, err := formatDb(fullDbBaseline)
				if err != nil {
					t.Fatalf("Cannot format db baseline: %s", err)
				}
				if doRebaseline() {
					runner.writeFile(runner.baselineDbPath, formattedDb)
				} else {
					runner.assertBaselineEquality(runner.baselineDbPath,
						string(formattedDb))
				}
			} else {
				runner.assertNoDbChanges(fullDbBaseline)
			}
		}

		if btest.Teardown != nil {
			err := btest.Teardown(t, &btest)
			if err != nil {
				t.Fatalf("Teardown failed: %s", err)
			}
		}
	})
}
