// Copyright 2014 Markus Dittrich. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
//
// test_engine contains the actual test functions analysing
// the output of the run MCell simulations
package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"regexp"
	"strings"
)

// testRunner analyses the TestDescriptions coming from an MCell run on a
// test and analyses them as requested per the TestDescription.
func testRunner(test *TestDescription, result chan *testResult) {
	for _, c := range test.Checks {

		dataPath := filepath.Join(test.Path, "output", c.DataFile)

		switch c.TestType {
		case "CHECK_SUCCESS":
			for _, testRun := range test.simStatus {
				if !testRun.success {
					message := strings.Join([]string{testRun.exitMessage, testRun.stdErrContent}, "\n")
					result <- &testResult{test.Path, false, "CHECK_SUCCESS", message}
					return // Special case - if simulation fails we won't continue testing
				} else {
					result <- &testResult{test.Path, true, "CHECK_SUCCESS", ""}
				}
			}

		case "CHECK_EXIT_CODE":
			for _, testRun := range test.simStatus {
				if c.ExitCode != testRun.exitCode {
					message := fmt.Sprintf("Expected exit code %d but got %d instead",
						c.ExitCode, testRun.exitCode)
					result <- &testResult{test.Path, false, "CHECK_EXIT_CODE", message}
				} else {
					result <- &testResult{test.Path, true, "CHECK_EXIT_CODE", ""}
				}
			}

		case "COUNT_CONSTRAINTS":
			err := checkCountConstraints(dataPath, c.HaveHeader, c.MinTime, c.MaxTime,
				c.CountConstraints)
			recordResult(result, "COUNT_CONSTRAINTS", test.Path, err)

		case "COUNT_MINMAX":
			err := checkCountMinmax(dataPath, c.HaveHeader, c.MinTime, c.MaxTime,
				c.CountMaximum, c.CountMinimum)
			recordResult(result, "COUNT_MINMAX", test.Path, err)

		case "FILE_MATCH_PATTERN":
			err := fileMatchPattern(dataPath, c.MatchPattern, c.NumMatches)
			recordResult(result, "FILE_MATCH_PATTERN", test.Path, err)

		case "COMPARE_COUNTS":
			referencePath := filepath.Join(test.Path, c.ReferenceFile)
			err := compareCounts(dataPath, referencePath, c.HaveHeader, c.MinTime,
				c.MaxTime)
			recordResult(result, "COMPARE_COUNTS", test.Path, err)

		case "COUNT_EQUILIBRIUM":
			err := checkCountEquilibrium(dataPath, c.HaveHeader, c.MinTime, c.MaxTime,
				c.Means, c.Tolerances)
			recordResult(result, "COUNT_EQUILIBRIUM", test.Path, err)

		case "POSITIVE_COUNTS":
			err := checkPositiveCounts(dataPath, c.HaveHeader, c.MinTime, c.MaxTime)
			recordResult(result, "POSITIVE_COUNTS", test.Path, err)

		default:
			recordResult(result, "----------------", test.Path,
				errors.New(fmt.Sprintf("Unknown test type: %s", c.TestType)))
		}
	}
}

// recordResults checks if a test was successfull or not, records
// success/failure in testResult object and sends it to the results channel
func recordResult(result chan<- *testResult, testType string, dataPath string,
	err error) {
	if err != nil {
		result <- &testResult{dataPath, false, testType, fmt.Sprint(err)}
	} else {
		result <- &testResult{dataPath, true, testType, ""}
	}
}

// checkCountConstraints tests the provided array of constraints
// on the simulation output data contained in the file filePath
func checkCountConstraints(filePath string, haveHeader bool, minTime, maxTime float64,
	constraints []*ConstraintSpec) error {

	// read data
	rows, err := readCounts(filePath, haveHeader)
	if err != nil {
		return err
	}

	// check constraints for each row of data
	for r, time := range rows.times {
		if (minTime > 0 && time < minTime) || (maxTime > 0 && time > maxTime) {
			continue
		}

		for _, con := range constraints {
			// sanity check - the number of columns has to match the number of constraints
			if len(con.Query) != len(rows.counts) {
				return errors.New(
					fmt.Sprintf("%s: length of constraints (%d) does not match number of data columns (%d)",
						filePath, len(rows.counts), len(con.Query)))
			}

			result := 0
			for c, q := range con.Query {
				result += (q * rows.counts[c][r])
			}

			if result != con.Target {
				return errors.New(
					fmt.Sprintf("constraint mismatch: result (%d) - actual (%d)", result, con.Target))
			}
		}
	}

	return nil
}

// checkCountMinmax tests that each column of the parsed data is larger
// equal than CountMinimum and smaller equal than CountMaximum.
func checkCountMinmax(filePath string, haveHeader bool, minTime, maxTime float64,
	countMaximum, countMinimum []int) error {

	// read data
	rows, err := readCounts(filePath, haveHeader)
	if err != nil {
		return err
	}

	if countMaximum != nil && len(countMaximum) != len(rows.counts) {
		return errors.New(
			"number of constraints in countMaximum does not match number of data columns")
	}

	if countMinimum != nil && len(countMinimum) != len(rows.counts) {
		return errors.New(
			"number of constraints in countMinimum does not match number of data columns")
	}

	for r, time := range rows.times {
		if (minTime > 0 && time < minTime) || (maxTime > 0 && time > maxTime) {
			continue
		}

		for i := 0; i < len(rows.counts); i++ {
			c := rows.counts[i][r]
			if countMaximum != nil && c > countMaximum[i] {
				return errors.New(
					fmt.Sprintf("maximum exceeded: data (%d) > max(%d)", c, countMaximum[i]))
			}
			if countMinimum != nil && c < countMinimum[i] {
				return errors.New(
					fmt.Sprintf("minimum undershot: data (%d) < min(%d)", c, countMinimum[i]))
			}
		}
	}

	return nil
}

// fileMatchPattern matches the provided matchPattern against the content of
// the datafile at filePath and checks that it matches numMatches times.
func fileMatchPattern(filePath string, matchPattern string,
	numExpectedMatches int) error {
	content, err := ioutil.ReadFile(filePath)
	if err != nil {
		return errors.New(
			fmt.Sprintf("failed to open file %s", filePath))
	}

	matcher := regexp.MustCompile(matchPattern)
	matches := matcher.FindAll(content, -1)
	if len(matches) != numExpectedMatches {
		return errors.New(
			fmt.Sprintf("failed pattern match: %s matched %d times instead of %d",
				matchPattern, len(matches), numExpectedMatches))
	}

	return nil
}

// compareCounts checks that the test data matches the provided column counts
// exactly
func compareCounts(dataPath, referencePath string, haveHeader bool, minTime,
	maxTime float64) error {

	// read data
	rows, err := readCounts(dataPath, haveHeader)
	if err != nil {
		return err
	}

	// read reference
	refRows, err := readCounts(referencePath, haveHeader)
	if err != nil {
		return err
	}

	if len(refRows.times) != len(rows.times) {
		return errors.New("reference and actual data set have different number of rows")
	}

	if len(refRows.counts) != len(rows.counts) {
		return errors.New("reference and actual data set have different number of columns")
	}

	numCols := len(rows.counts)
	for r, time := range rows.times {
		if (minTime > 0 && time < minTime) || (maxTime > 0 && time > maxTime) {
			continue
		}

		for c := 0; c < numCols; c++ {
			if rows.counts[c][r] != refRows.counts[c][r] {
				return errors.New(
					fmt.Sprintf("reference and actual data differ in row %d and col %d", r, c))
			}
		}
	}

	return nil
}

// checkCountEqulibrium checks that the column means of the test data match the
// provided target mean values withih tne provided tolerances.
func checkCountEquilibrium(dataPath string, haveHeader bool, minTime, maxTime float64,
	means, tolerances []float64) error {

	// read data
	rows, err := readCounts(dataPath, haveHeader)
	if err != nil {
		return err
	}

	if len(means) != len(rows.counts) {
		return errors.New("number of provided means does not match number of data columns")
	}

	numCols := len(rows.counts)
	averages := make([]float64, numCols)
	var numValues int
	for r, time := range rows.times {
		if (minTime > 0 && time < minTime) || (maxTime > 0 && time > maxTime) {
			continue
		}

		numValues += 1
		for c := 0; c < numCols; c++ {
			averages[c] += float64(rows.counts[c][r])
		}
	}

	// compare averages with target means
	for c := 0; c < numCols; c++ {
		average := averages[c] / float64(numValues)
		if (average < means[c]-tolerances[c]) || (average > means[c]+tolerances[c]) {
			return errors.New(
				fmt.Sprintf("average value %f of column %d outside of tolerance %f +/- %f",
					average, c, means[c], tolerances[c]))
		}
	}

	return nil

}

// checkPosititveCounts tests that all counts of the data file are positive > 0
func checkPositiveCounts(dataPath string, haveHeader bool, minTime,
	maxTime float64) error {

	// read data
	rows, err := readCounts(dataPath, haveHeader)
	if err != nil {
		return err
	}

	numCols := len(rows.counts)
	for r, time := range rows.times {
		if (minTime > 0 && time < minTime) || (maxTime > 0 && time > maxTime) {
			continue
		}

		for c := 0; c < numCols; c++ {
			if rows.counts[c][r] <= 0 {
				return errors.New(
					fmt.Sprintf("value %d in column %d in row %d is not positive (<= 0)",
						rows.counts[c][r], c, r))
			}
		}
	}

	return nil
}