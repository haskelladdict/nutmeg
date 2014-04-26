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

		case "COUNT_CONSTRAINTS":
			success, err := checkCountConstraints(dataPath, c.HaveHeader, c.MinTime,
				c.CountConstraints)
			if !success || err != nil {
				result <- &testResult{test.Path, false, "COUNT_CONSTRAINTS", fmt.Sprint(err)}
			} else {
				result <- &testResult{test.Path, true, "COUNT_CONSTRAINTS", ""}
			}

		case "COUNT_MINMAX":
			success, err := checkCountMinmax(dataPath, c.HaveHeader, c.MinTime,
				c.CountMaximum, c.CountMinimum)
			if !success || err != nil {
				result <- &testResult{test.Path, false, "COUNT_MINMAX", fmt.Sprint(err)}
			} else {
				result <- &testResult{test.Path, true, "COUNT_MINMAX", ""}
			}

		case "FILE_MATCH_PATTERN":
			success, err := fileMatchPattern(dataPath, c.MatchPattern, c.NumMatches)
			if !success || err != nil {
				result <- &testResult{test.Path, false, "FILE_MATCH_PATTERN", fmt.Sprint(err)}
			} else {
				result <- &testResult{test.Path, true, "FILE_MATCH_PATTERN", ""}
			}
		}
	}
}

// checkCountConstraints tests the provided array of constraints
// on the simulation output data contained in the file filePath
func checkCountConstraints(filePath string, haveHeader bool, minTime float64,
	constraints []*ConstraintSpec) (bool, error) {

	// read data
	rows, err := readCounts(filePath, haveHeader)
	if err != nil {
		return false, err
	}

	// check constraints for each row of data
	for r, time := range rows.times {
		if time < minTime {
			continue
		}

		for _, con := range constraints {
			// sanity check - the number of columns has to match the number of constraints
			if len(con.Query) != len(rows.counts) {
				return false, errors.New(
					fmt.Sprintf("%s: length of constraints (%d) does not match number of data columns (%d)",
						filePath, len(rows.counts), len(con.Query)))
			}

			result := 0
			for c, q := range con.Query {
				result += (q * rows.counts[c][r])
			}

			if result != con.Target {
				return false, errors.New(
					fmt.Sprintf("constraint mismatch: result (%d) - actual (%d)", result, con.Target))
			}
		}
	}

	return true, nil
}

// checkCountMinmax tests that each column of the parsed data is larger
// equal than CountMinimum and smaller equal than CountMaximum.
func checkCountMinmax(filePath string, haveHeader bool, minTime float64,
	countMaximum, countMinimum []int) (bool, error) {

	// read data
	rows, err := readCounts(filePath, haveHeader)
	if err != nil {
		return false, err
	}

	for r, time := range rows.times {
		if time < minTime {
			continue
		}

		for i := 0; i < len(rows.counts); i++ {
			c := rows.counts[i][r]
			if countMaximum != nil && c > countMaximum[i] {
				return false, errors.New(
					fmt.Sprintf("maximum exceeded: data (%d) > max(%d) %f %f", c, countMaximum[i],
						time, minTime))
			}
			if countMinimum != nil && c < countMinimum[i] {
				return false, errors.New(
					fmt.Sprintf("minimum undershot: data (%d) < max(%d)", c, countMaximum[i]))
			}
		}
	}

	return true, nil
}

// fileMatchPattern matches the provided matchPattern against the content of
// the datafile at filePath and checks that it matches numMatches times.
func fileMatchPattern(filePath string, matchPattern string,
	numExpectedMatches int) (bool, error) {
	content, err := ioutil.ReadFile(filePath)
	if err != nil {
		return false, errors.New(
			fmt.Sprintf("failed to open file %s", filePath))
	}

	matcher := regexp.MustCompile(matchPattern)
	matches := matcher.FindAll(content, -1)
	if len(matches) != numExpectedMatches {
		return false, errors.New(
			fmt.Sprintf("failed pattern match: %s matched %d times instead of %d",
				matchPattern, len(matches), numExpectedMatches))
	}

	return true, nil
}