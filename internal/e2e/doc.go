// Package e2e holds fixture-driven end-to-end tests that run the real
// loader → validate → resolve → render pipeline over the projects in testdata/.
//
// See testdata/README.txt for the fixture format. Regenerate golden files with:
//
//	go test ./internal/e2e -update
package e2e
