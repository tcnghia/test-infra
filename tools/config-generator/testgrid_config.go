/*
Copyright 2019 The Knative Authors
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// data definitions that are used for the testgrid config file generation

package main

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	// baseOptions setting for testgrid dashboard tabs
	testgridTabGroupByDir     = "exclude-filter-by-regex=Overall$&group-by-directory=&expand-groups=&sort-by-name="
	testgridTabGroupByTarget  = "exclude-filter-by-regex=Overall$&group-by-target=&expand-groups=&sort-by-name="
	testgridTabSortByName     = "sort-by-name="
	testgridTabSortByFailures = "sort-by-failures="

	// generalTestgridConfig contains config-wide definitions.
	generalTestgridConfig = "testgrid_config_header.yaml"

	// testGroupTemplate is the template for the test group config
	testGroupTemplate = "testgrid_testgroup.yaml"

	// dashboardTabTemplate is the template for the dashboard tab config
	dashboardTabTemplate = "testgrid_dashboardtab.yaml"

	// dashboardGroupTemplate is the template for the dashboard tab config
	dashboardGroupTemplate = "testgrid_dashboardgroup.yaml"
)

var (
	// goCoverageMap keep track of which repo has go code coverage when parsing the simple config file
	goCoverageMap map[string]bool

	metaData = NewTestGridMetaData()

	// templatesCache caches templates in memory to avoid I/O
	templatesCache        = make(map[string]string)
	quotedEmailPattern, _ = regexp.Compile("\"(.+@.+\\..+)\"")
)

// baseTestgridTemplateData contains basic data about the testgrid config file.
// TODO(chizhg): remove this structure and use baseProwJobTemplateData instead
type baseTestgridTemplateData struct {
	ProwHost          string
	TestGridHost      string
	GubernatorHost    string
	TestGridGcsBucket string
	TestGroupName     string
	Year              int
}

// testGroupTemplateData contains data about a test group
type testGroupTemplateData struct {
	Base baseTestgridTemplateData
	// TODO(chizhg): use baseProwJobTemplateData then this attribute can be removed
	GcsLogDir string
	Extras    map[string]string
}

// dashboardTabTemplateData contains data about a dashboard tab
type dashboardTabTemplateData struct {
	Base        baseTestgridTemplateData
	Name        string
	BaseOptions string
	Extras      map[string]string
}

// dashboardGroupTemplateData contains data about a dashboard group
type dashboardGroupTemplateData struct {
	Name      string
	RepoNames []string
}

// testgridEntityGenerator is a function that generates the entity given the repo name and job names
type testgridEntityGenerator func(string, string, []string)

// newBaseTestgridTemplateData returns a testgridTemplateData type with its initial, default values.
func newBaseTestgridTemplateData(testGroupName string) baseTestgridTemplateData {
	var data baseTestgridTemplateData
	data.Year = time.Now().Year()
	data.ProwHost = prowHost
	data.TestGridHost = testGridHost
	data.GubernatorHost = gubernatorHost
	data.TestGridGcsBucket = testGridGcsBucket
	data.TestGroupName = testGroupName
	return data
}

// Get returns the project JobDetailMap, creating it if necessary
func (t *TestGridMetaData) Get(projName string) JobDetailMap {
	t.EnsureExists(projName)
	return t.md[projName]
}

func (t *TestGridMetaData) EnsureExists(projName string) bool {
	if _, exists := t.md[projName]; !exists {
		t.md[projName] = make(JobDetailMap)
		if !strExists(t.projNames, projName) {
			t.projNames = append(t.projNames, projName)
		}
		return false
	}
	return true
}

func (t *TestGridMetaData) EnsureRepo(projName, repoName string) bool {
	jdm := t.Get(projName)
	if !jdm.EnsureExists(repoName) {
		if !strExists(t.repoNames, repoName) {
			t.repoNames = append(t.repoNames, repoName)
		}
		return false
	}
	return true
}

// generateTestGridSection generates the configs for a TestGrid section using the given generator
func (t *TestGridMetaData) generateTestGridSection(sectionName string, generator testgridEntityGenerator, skipReleasedProj bool) {
	oldCount := output.count
	output.outputConfig(sectionName + ":")
	for _, projName := range t.projNames {
		// Do not handle the project if it is released and we want to skip it.
		if skipReleasedProj && isReleased(projName) {
			continue
		}
		repos := t.md[projName]
		for _, repoName := range t.repoNames {
			if jobNames, exists := repos[repoName]; exists {
				generator(projName, repoName, jobNames)
			}
		}
	}
	// A TestGrid config cannot have an empty section, so add a bogus entry
	// if nothing was generated, thus the config is semantically valid.
	if output.count == oldCount {
		output.outputConfig(baseIndent + "- name: empty")
	}
}

// generateNonAlignedTestGroups
func (t *TestGridMetaData) generateNonAlignedTestGroups() {
	for _, tg := range t.nonAligned {
		executeTestGroupTemplate(tg.CIJobName, getGcsLogDir(tg.CIJobName), tg.Extra)
	}
}

//
// testGroupName: This is the human-readable tab name
func (t *TestGridMetaData) AddNonAlignedTest(n NonAlignedTestGroup) {
	t.nonAligned = append(t.nonAligned, n)
}

// testGroupName: the name of the job in every case AFAICT
func getGcsLogDir(testGroupName string) string {
	return fmt.Sprintf("%s/%s/%s", GCSBucket, LogsDir, testGroupName)
}

func getTestgroupExtras(projName, jobName string) map[string]string {
	extras := make(map[string]string)
	switch jobName {
	case "continuous":
		// projName has the release encoded into it, so the main page at http://testgrid.knative.dev
		// does not mix releases with the master branch
		if releaseRegex.FindString(projName) != "" {
			extras["num_failures_to_alert"] = "3"
			extras["alert_options"] = "\n    alert_mail_to_addresses: \"serverless-engprod-sea@google.com\""
		} else {
			extras["alert_stale_results_hours"] = "3"
		}
	case "dot-release", "auto-release", "nightly":
		extras["num_failures_to_alert"] = "1"
		extras["alert_options"] = "\n    alert_mail_to_addresses: \"serverless-engprod-sea@google.com\""
		if jobName == "dot-release" {
			extras["alert_stale_results_hours"] = "170" // 1 week + 2h
		}
	case "webhook-apicoverage":
		extras["alert_stale_results_hours"] = "48" // 2 days
	case "test-coverage":
		extras["short_text_metric"] = "coverage"
	default:
		extras["alert_stale_results_hours"] = "3"
	}
	return extras
}

func generateProwJobAnnotations(repoName, jobName string, tgExtras map[string]string) []string {
	annotations := []string{
		"  testgrid-dashboards: " + repoName,
		"  testgrid-tab-name: " + jobName,
	}

	v, ok := tgExtras["alert_stale_results_hours"]
	if ok {
		res := fmt.Sprintf("  testgrid-alert-stale-results-hours: \"%s\"", v)
		annotations = append(annotations, res)
	}
	v, ok = tgExtras["short_text_metric"]
	if ok {
		res := "  testgrid-in-cell-metric: " + v
		annotations = append(annotations, res)
	}
	v, ok = tgExtras["alert_options"]
	if ok {
		email := quotedEmailPattern.FindStringSubmatch(v)[1] //index 1 is first capture group
		res := fmt.Sprintf("  testgrid-alert-email: \"%s\"", email)
		annotations = append(annotations, res)
	}
	v, ok = tgExtras["num_failures_to_alert"]
	if ok {
		res := fmt.Sprintf("  testgrid-num-failures-to-alert: \"%s\"", v)
		annotations = append(annotations, res)
	}
	return annotations
}

// generateTestGroup generates the test group configuration
func (t *TestGridMetaData) generateTestGroup(projName string, repoName string, jobNames []string) {
	projRepoStr := buildProjRepoStr(projName, repoName)
	for _, jobName := range jobNames {
		testGroupName := getTestGroupName(projRepoStr, jobName)
		testGroupNameForGCSLogDir := testGroupName
		if jobName == "test-coverage" {
			testGroupNameForGCSLogDir = fmt.Sprintf("ci-%s-%s", projRepoStr, "go-coverage")
		}
		gcsLogDir := getGcsLogDir(testGroupNameForGCSLogDir)
		extras := getTestgroupExtras(projName, jobName)
		executeTestGroupTemplate(testGroupName, gcsLogDir, extras)
	}
}

// executeTestGroupTemplate outputs the given test group config template with the given data
func executeTestGroupTemplate(testGroupName string, gcsLogDir string, extras map[string]string) {
	var data testGroupTemplateData
	data.Base.TestGroupName = testGroupName
	data.GcsLogDir = gcsLogDir
	data.Extras = extras
	executeTemplate("test group", readTemplate(testGroupTemplate), data)
}

// generateDashboard generates the dashboard configuration
func generateDashboard(projName string, repoName string, jobNames []string) {
	projRepoStr := buildProjRepoStr(projName, repoName)
	output.outputConfig("- name: " + strings.ToLower(repoName) + "\n" + baseIndent + "dashboard_tab:")
	noExtras := make(map[string]string)
	for _, jobName := range jobNames {
		testGroupName := getTestGroupName(projRepoStr, jobName)
		switch jobName {
		case "continuous":
			extras := make(map[string]string)
			extras["num_failures_to_alert"] = "3"
			extras["alert_options"] = "\n      alert_mail_to_addresses: \"serverless-engprod-sea@google.com\""
			executeDashboardTabTemplate("continuous", testGroupName, testgridTabSortByName, extras)
			// This is a special case for knative/serving, as conformance tab is just a filtered view of the continuous tab.
			if projRepoStr == "knative-serving" {
				executeDashboardTabTemplate("conformance", testGroupName, "include-filter-by-regex=test/conformance/&sort-by-name=", extras)
			}
		case "dot-release", "auto-release":
			extras := make(map[string]string)
			extras["num_failures_to_alert"] = "1"
			extras["alert_options"] = "\n      alert_mail_to_addresses: \"serverless-engprod-sea@google.com\""
			baseOptions := testgridTabSortByName
			executeDashboardTabTemplate(jobName, testGroupName, baseOptions, extras)
		case "webhook-apicoverage":
			baseOptions := testgridTabSortByName
			executeDashboardTabTemplate(jobName, testGroupName, baseOptions, noExtras)
		case "nightly":
			extras := make(map[string]string)
			extras["num_failures_to_alert"] = "1"
			extras["alert_options"] = "\n      alert_mail_to_addresses: \"serverless-engprod-sea@google.com\""
			executeDashboardTabTemplate("nightly", testGroupName, testgridTabSortByName, extras)
		case "test-coverage":
			executeDashboardTabTemplate("coverage", testGroupName, testgridTabGroupByDir, noExtras)
		default:
			executeDashboardTabTemplate(jobName, testGroupName, testgridTabSortByName, noExtras)
		}
	}
}

// executeTestGroupTemplate outputs the given dashboard tab config template with the given data
func executeDashboardTabTemplate(dashboardTabName string, testGroupName string, baseOptions string, extras map[string]string) {
	var data dashboardTabTemplateData
	data.Name = dashboardTabName
	data.Base.TestGroupName = testGroupName
	data.BaseOptions = baseOptions
	data.Extras = extras
	executeTemplate("dashboard tab", readTemplate(dashboardTabTemplate), data)
}

// getTestGroupName get the testGroupName from the given repoName and jobName
func getTestGroupName(repoName string, jobName string) string {
	switch jobName {
	case "nightly":
		return strings.ToLower(fmt.Sprintf("ci-%s-%s-release", repoName, jobName))
	default:
		return strings.ToLower(fmt.Sprintf("ci-%s-%s", repoName, jobName))
	}
}

// generateNonAlignedDashboards generates some of the content under "dashboards:"
func (t *TestGridMetaData) generateNonAlignedDashboards() {
	// Collect them by DashboardName
	var keys []string
	dn := make(map[string][]NonAlignedTestGroup)
	for _, tg := range t.nonAligned {
		_, exists := dn[tg.DashboardName]
		if !exists {
			dn[tg.DashboardName] = make([]NonAlignedTestGroup, 0)
			keys = append(keys, tg.DashboardName)
		}
		dn[tg.DashboardName] = append(dn[tg.DashboardName], tg)
	}
	for _, name := range keys {
		tgs := dn[name]
		output.outputConfig("- name: " + name + "\n" + baseIndent + "dashboard_tab:")
		for _, tg := range tgs {
			executeDashboardTabTemplate(tg.HumanTabName, tg.CIJobName, tg.BaseOptions, nil)
		}
	}
}

// generateDashboardsForReleases generates some of the content under "dashboards:"
func (t *TestGridMetaData) generateDashboardsForReleases() {
	for _, projName := range t.projNames {
		// Do not handle the project if it is not released.
		if !isReleased(projName) {
			continue
		}
		repos := t.md[projName]
		output.outputConfig("- name: " + projName + "\n" + baseIndent + "dashboard_tab:")
		for _, repoName := range t.repoNames {
			if jobNames, exists := repos[repoName]; exists {
				for _, jobName := range jobNames {
					extras := make(map[string]string)
					extras["num_failures_to_alert"] = "3"
					extras["alert_options"] = "\n      alert_mail_to_addresses: \"serverless-engprod-sea@google.com\""
					testGroupName := getTestGroupName(buildProjRepoStr(projName, repoName), jobName)
					executeDashboardTabTemplate(repoName+"-"+jobName, testGroupName, testgridTabSortByName, extras)
				}
			}
		}
	}
}

// generateNonAlignedDashboardGroups generates some of the content under "dashboards:"
func (t *TestGridMetaData) generateNonAlignedDashboardGroups() {
	// Collect Dashboards by DashboardGroup
	var keys []string
	dg := make(map[string][]string)
	for _, tg := range t.nonAligned {
		_, exists := dg[tg.DashboardGroup]
		if !exists {
			dg[tg.DashboardGroup] = make([]string, 0)
			keys = append(keys, tg.DashboardGroup)
		}
		if !strExists(dg[tg.DashboardGroup], tg.DashboardName) {
			dg[tg.DashboardGroup] = append(dg[tg.DashboardGroup], tg.DashboardName)
		}
	}
	for _, group := range keys {
		names := dg[group]
		executeDashboardGroupTemplate(group, names)
	}
}

// generateDashboardGroups generates the stuff in dashboard_groups:
func (t *TestGridMetaData) generateDashboardGroups() {
	output.outputConfig("dashboard_groups:")
	for _, projName := range t.projNames {
		// there is only one dashboard for each released project, so we do not need to group them
		if isReleased(projName) {
			continue
		}

		dashboardRepoNames := make([]string, 0)
		repos := t.md[projName]
		for _, repoName := range t.repoNames {
			if _, exists := repos[repoName]; exists {
				dashboardRepoNames = append(dashboardRepoNames, repoName)
			}
		}
		executeDashboardGroupTemplate(projName, dashboardRepoNames)
	}
}

// executeDashboardGroupTemplate outputs the given dashboard group config template with the given data
func executeDashboardGroupTemplate(dashboardGroupName string, dashboardRepoNames []string) {
	var data dashboardGroupTemplateData
	data.Name = dashboardGroupName
	data.RepoNames = dashboardRepoNames
	executeTemplate("dashboard group", readTemplate(dashboardGroupTemplate), data)
}
