package httpbaselinetest

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"regexp"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/pmezard/go-difflib/difflib"
)

type HttpBaselineTestSuite struct {
	t           *testing.T
	baselineDir string
}

func NewDefaultHttpBaselineTestSuite(t *testing.T) *HttpBaselineTestSuite {
	return &HttpBaselineTestSuite{
		t:           t,
		baselineDir: "testdata",
	}
}

type HttpBaselineTestSetupFunc func(testName string, baselineTest *HttpBaselineTest) error
type HttpBaselineTestTeardownFunc func(t *testing.T, baselineTest *HttpBaselineTest) error
type HttpBaselineTestSeedFunc func(baselineTest *HttpBaselineTest) error
type HttpBaselineTestBodyValidatorFunc func(body []byte) error
type HttpBaselineTest struct {
	Setup    HttpBaselineTestSetupFunc
	Teardown HttpBaselineTestTeardownFunc
	Custom   interface{}

	Handler           http.Handler
	Method            string
	Path              string
	Body              interface{} // io.Reader or string
	Headers           map[string]string
	RequestValidator  HttpBaselineTestBodyValidatorFunc
	ResponseValidator HttpBaselineTestBodyValidatorFunc

	Db       *sqlx.DB
	Seed     string
	SeedFunc HttpBaselineTestSeedFunc
	Tables   []string
}

type httpBaselineTestRunner struct {
	testName         string
	suite            *HttpBaselineTestSuite
	btest            *HttpBaselineTest
	t                *testing.T
	baselineReqPath  string
	baselineRespPath string
	baselineDbPath   string
	seedPath         string
	dbTableInfo      *dbTableInfo
}

func newRunner(testName string, t *testing.T, suite *HttpBaselineTestSuite,
	btest *HttpBaselineTest) httpBaselineTestRunner {
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

func formatJson(body []byte) (string, error) {
	var v interface{}
	err := json.Unmarshal(body, &v)
	if err != nil {
		return "", err
	}
	pj, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(pj), nil
}

func formatBody(contentType string, body []byte) (string, error) {
	if contentType == "application/json" {
		return formatJson(body)
	} else {
		return string(body), nil
	}
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
	err := ioutil.WriteFile(path, []byte(formattedData), 0644)
	if err != nil {
		r.t.Fatalf("Error writing baseline %s: %s", path, err)
	}
}

func (suite *HttpBaselineTestSuite) Run(name string, btest HttpBaselineTest) {
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
			err := btest.RequestValidator(rawReqBody)
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
