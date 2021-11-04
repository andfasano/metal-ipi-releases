package main

import (
	"bufio"
	"bytes"
	"encoding/gob"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	// This is the url where the Prow jobs artifacts are stored
	baseUrl = "https://gcsweb-ci.apps.ci.l2s4.p1.openshiftapps.com/gcs/origin-ci-test/logs"
)

var (
	ignoreTestCases = map[string]struct{}{
		"[sig-arch] Monitor cluster while tests execute": {},
	}
)

// Every job will publish a finished.json artifact when completed
type Finished struct {
	Timestamp int64  `json:"timestamp"`
	Passed    bool   `json:"passed"`
	Result    string `json:"result"`
	Revision  string `json:"revision"`
}

type Build struct {
	// The job owner of this build
	job *Job
	// The unique build id
	id string
	// The end status of the build
	finished Finished
	// A link to the build artifacts
	artifactsUrl string
}

func (b *Build) fetchRemoteFile(url string) ([]byte, error) {
	r, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}

	return body, nil

}

func (b *Build) fetchTestStepResult() error {
	url := fmt.Sprintf("%s/baremetalds-e2e-test/finished.json", b.artifactsUrl)
	body, err := b.fetchRemoteFile(url)

	err = json.Unmarshal(body, &b.finished)
	if err != nil {
		return err
	}

	return nil
}

type TestCaseSkipped struct {
	XMLName xml.Name `xml:"skipped"`
	Message string   `xml:"message,attr"`
}

type TestCase struct {
	XMLName   xml.Name        `xml:"testcase"`
	Name      string          `xml:"name,attr"`
	Skipped   TestCaseSkipped `xml:"skipped"`
	Failure   string          `xml:"failure"`
	SystemOut string          `xml:"system-out"`
}

func (tc *TestCase) IsSkipped() bool {
	return tc.Skipped.Message != ""
}

func (tc *TestCase) IsFailure() bool {
	return tc.Failure != ""
}

func (tc *TestCase) IsPassed() bool {
	return !tc.IsFailure()
}

func (tc *TestCase) Ignore() bool {
	_, ok := ignoreTestCases[tc.Name]
	return ok
}

type TestProperty struct {
	XMLName xml.Name `xml:"property"`
	Name    string   `xml:"name,attr"`
	Value   string   `xml:"value,attr"`
}

type TestSuite struct {
	XMLName  xml.Name `xml:"testsuite"`
	Name     string   `xml:"name,attr"`
	Tests    int      `xml:"tests,attr"`
	Skipped  int      `xml:"skipped,attr"`
	Failures int      `xml:"failures,attr"`
	Time     int      `xml:"time,attr"`

	Property TestProperty `xml:"property"`

	TestCases []TestCase `xml:"testcase"`
}

// Scraping test filename, since it contains a timestamp
func (b *Build) getTestsXmlFilename(testsUrl string) (string, error) {
	r, err := http.Get(testsUrl)
	if err != nil {
		return "", err
	}
	defer r.Body.Close()

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return "", err
	}

	re := regexp.MustCompile(`<div class="pure-u-2-5">.*<img src="/icons/file.png"> (junit_.*\.xml)`)
	matches := re.FindStringSubmatch(string(body))
	if matches == nil {
		return "", fmt.Errorf("Test file not found or missing")
	}

	return fmt.Sprintf("%s%s", testsUrl, matches[1]), nil
}

// FetchTestsXml retrieve the junit xml test for the current build
func (b *Build) FetchTestsXml() (*TestSuite, error) {

	testsUrl := fmt.Sprintf("%s/%s/%s/artifacts/%s/baremetalds-e2e-test/artifacts/junit/", baseUrl, b.job.name, b.id, b.job.safeName)

	testXmlUrl, err := b.getTestsXmlFilename(testsUrl)
	if err != nil {
		return nil, err
	}

	body, err := b.fetchRemoteFile(testXmlUrl)

	testSuite := TestSuite{}
	err = xml.Unmarshal(body, &testSuite)
	if err != nil {
		return nil, err
	}

	return &testSuite, nil
}

func NewBuild(id string, job *Job) *Build {
	return &Build{
		id:           id,
		job:          job,
		artifactsUrl: fmt.Sprintf("%s/%s/%s/artifacts/%s", baseUrl, job.name, id, job.safeName),
	}
}

//-----------------------------------------------------------------------------
// TestHistory is used to accumulate the detected flakes for given test
type TestHistory struct {
	PreviousState bool
	Flakes        float32
}

// JobHistory keeps all the relevant info for the analyzed builds
// for a given job
type JobHistory struct {
	From        int64
	To          int64
	TotalBuilds float32
	Data        map[string]TestHistory
}

// Job represent a Prow job
type Job struct {
	name     string
	safeName string
	builds   []*Build
	history  JobHistory
}

func NewJob(name string) *Job {
	return &Job{
		name:     name,
		safeName: name[strings.Index(name, "e2e"):],
		builds:   []*Build{},
		history: JobHistory{
			Data: make(map[string]TestHistory),
		},
	}
}

// ListBuilds select the last N builds, for a given job.
// For convenience, build ids are scraped directly from the jobs url
func (j *Job) ListBuilds(numBuilds int) error {
	log.Print(j.name, " - Listing builds")
	buildsUrl := fmt.Sprintf("%s/%s/", baseUrl, j.name)

	r, err := http.Get(buildsUrl)
	if err != nil {
		return err
	}
	defer r.Body.Close()

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return err
	}

	buildIds := []string{}
	re := regexp.MustCompile(`<div class="pure-u-2-5">.*<img src="/icons/dir.png"> (\d+)`)
	matches := re.FindAllStringSubmatch(string(body), -1)
	for _, m := range matches {
		buildIds = append(buildIds, m[1])
	}
	sort.Strings(buildIds)

	// Fetch last N builds
	j.builds = []*Build{}
	totalBuilds := len(buildIds)
	if totalBuilds < numBuilds {
		numBuilds = len(buildIds)
	}
	for n := totalBuilds - 1; ; n-- {

		b := NewBuild(buildIds[n], j)
		err := b.fetchTestStepResult()
		// Select only finished builds
		if err == nil {
			j.builds = append(j.builds, b)
		}
		if len(j.builds) >= numBuilds {
			break
		}
	}

	log.Printf("%s - Found %d build, selected last %d", j.name, len(buildIds), len(j.builds))

	return nil
}

// ParseTests scans the test results for flakes
func (j *Job) ParseTests() error {

	log.Printf("%s - Parsing tests for builds [%s, %s]", j.name, j.builds[0].id, j.builds[len(j.builds)-1].id)

	// Counting intermittent failures for all the builds
	for _, b := range j.builds {

		// Skip builds without tests
		suite, err := b.FetchTestsXml()
		if err != nil {
			continue
		}

		for _, tc := range suite.TestCases {

			if tc.Ignore() {
				continue
			}

			thc, ok := j.history.Data[tc.Name]
			if !ok {
				thc = TestHistory{
					PreviousState: true,
				}
			}

			if tc.IsPassed() != thc.PreviousState {
				thc.Flakes += 0.5
			}
			thc.PreviousState = tc.IsPassed()

			j.history.Data[tc.Name] = thc
		}

		j.history.TotalBuilds += 1.0
	}

	j.history.To = j.builds[0].finished.Timestamp
	j.history.From = j.builds[len(j.builds)-1].finished.Timestamp

	return nil
}

func (j *Job) dataFilename() string {
	return fmt.Sprintf("%s.raw", j.name)
}

// Save the parsed data to file
func (j *Job) Serialize() {
	log.Println(j.name, "- Saving data")
	buff := new(bytes.Buffer)
	encoder := gob.NewEncoder(buff)

	err := encoder.Encode(j.history)
	if err != nil {
		log.Println(j.name, "- Error while serializing data", err.Error())
		return
	}

	data, err := os.Create(j.dataFilename())
	if err != nil {
		log.Println(j.name, "- Error while creating file", err.Error())
		return
	}
	defer data.Close()

	w := bufio.NewWriter(data)
	w.Write(buff.Bytes())
}

// If cached data are found, let's reuse them
func (j *Job) Deserialize() bool {
	data, err := os.Open(j.dataFilename())
	if err != nil {
		return false
	}
	defer data.Close()

	r := bufio.NewReader(data)
	decoder := gob.NewDecoder(r)
	err = decoder.Decode(&j.history)
	if err != nil {
		log.Println(j.name, "- Error while deserializing data", err.Error())
	}

	return true
}

func (j *Job) ShowIntermittentFailures() {

	type FlakyTest struct {
		name      string
		flakiness float32
	}

	flakes := []FlakyTest{}
	for k, v := range j.history.Data {
		if v.Flakes == 0.0 {
			continue
		}

		flakiness := v.Flakes / j.history.TotalBuilds
		flakes = append(flakes, FlakyTest{
			name:      k,
			flakiness: flakiness,
		})
	}

	sort.Slice(flakes, func(i, j int) bool {
		return flakes[i].flakiness > flakes[j].flakiness
	})

	to := time.Unix(j.history.To, 0).UTC()
	from := time.Unix(j.history.From, 0).UTC()
	fmt.Println("-----------------------------------------")
	fmt.Printf("\n[%s] Top flaky tests (last %0.f days, %0.f builds)\n", j.name, to.Sub(from).Hours()/24, j.history.TotalBuilds)
	for _, f := range flakes {
		fmt.Printf("%0.2f\t%s\n", f.flakiness, f.name)
	}
}

//-----------------------------------------------------------------------------

func main() {

	jobsFmt := []string{
		"periodic-ci-openshift-release-master-nightly-%s-e2e-metal-ipi",
		// "periodic-ci-openshift-release-master-nightly-%s-e2e-metal-ipi-ovn-ipv6",
		// "periodic-ci-openshift-release-master-nightly-%s-e2e-metal-ipi-serial-ipv4",
		// "periodic-ci-openshift-release-master-nightly-%s-e2e-metal-ipi-virtualmedia",
		// "periodic-ci-openshift-release-master-nightly-%s-e2e-metal-ipi-ovn-dualstack",
		// "periodic-ci-openshift-release-master-nightly-%s-e2e-metal-ipi-compact",
		// "periodic-ci-openshift-release-master-nightly-%s-e2e-metal-ipi-upgrade",
	}

	versions := []string{
		"4.10",
	}

	numBuilds := 10

	for _, v := range versions {
		for _, jf := range jobsFmt {
			job := NewJob(fmt.Sprintf(jf, v))
			if !job.Deserialize() {
				job.ListBuilds(numBuilds)

				err := job.ParseTests()
				if err != nil {
					log.Fatal(err)
				}
				job.Serialize()
			}
			job.ShowIntermittentFailures()
		}
	}
}
