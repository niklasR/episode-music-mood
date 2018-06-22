package main

import (
	"testing"

	gock "gopkg.in/h2non/gock.v1"
)

func TestGetVersionID(t *testing.T) {
	defer gock.Off() // Flush pending mocks after test execution
	gock.New("http://ibl.api.bbci.co.uk").
		Get("/ibl/v1/episodes/epid1?availability=all\u0026mixin=live").
		Reply(200).
		File("fixtures/episode.json")

	versionID, err := getVersionID("epid1")
	if err != nil {
		t.Fail()
	}
	if versionID != "vpid1" {
		t.Fail()
	}

}
