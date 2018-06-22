package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"os"
	"reflect"
	"strings"

	"github.com/tkanos/gonfig"
	"github.com/zmb3/spotify"
	"golang.org/x/oauth2/clientcredentials"
)

type configuration struct {
	CertFile            string `json:"certFile"`
	KeyFile             string `json:"keyFile"`
	SpotifyClientID     string `json:"spotifyClientId"`
	SpotifyClientSecret string `json:"spotifyClientSecret"`
	MusicURL            string `json:"musicUrl"`
	IBLUrl              string `json:"iblUrl"`
	PlaylisterURL       string `json:"playlisterUrl"`
}

type errorMessage struct {
	Error string `json:"error"`
}

type version struct {
	ID string `json:"id"`
}

type episode struct {
	Versions []version `json:"versions"`
}

type iblEpisodesResponse struct {
	Episodes []episode `json:"episodes"`
}

type playlisterSegment struct {
	RecordID string `json:"record_id"`
}

type playlisterResponse struct {
	Segments []playlisterSegment `json:"segments"`
}

type externalLink struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type musicData struct {
	ExternalLinks []externalLink `json:"external-links"`
}

type musicResponse struct {
	Data musicData `json:"data"`
}

type spotifyTrackData struct {
	Features spotify.AudioFeatures
	Analysis spotify.AudioAnalysis
}

type mood struct {
	ChillFactor     float32 `json:"chillFactor"`
	HappinessFactor float32 `json:"happinessFactor"`
}

func getConfiguration() configuration {
	configuration := configuration{}
	err := gonfig.GetConf("./config.json", &configuration)
	if err != nil {
		returnError(err.Error())
	}

	v := reflect.ValueOf(configuration)
	values := make([]interface{}, v.NumField())
	for i := 0; i < v.NumField(); i++ {
		values[i] = v.Field(i).Interface()
	}
	for _, val := range values {
		strVal := fmt.Sprintln(val)
		if !(len(strVal) > 1) {
			returnError("Config incomplete")
		}
	}
	return configuration
}

func getRecordIDs(versionID string) []string {
	configuration := getConfiguration()
	url := fmt.Sprintf(configuration.PlaylisterURL, versionID)

	res := playlisterResponse{}
	httpRes, err := http.Get(url)
	if err != nil {
		returnError("Failed to get Record IDs")
	}
	body, _ := ioutil.ReadAll(httpRes.Body)

	json.Unmarshal(body, &res)

	var recordIds []string

	for _, segment := range res.Segments {
		recordIds = append(recordIds, segment.RecordID)
	}

	return recordIds
}

func getVersionID(episodeID string) (string, error) {
	configuration := getConfiguration()
	url := fmt.Sprintf(configuration.IBLUrl, episodeID)

	epResp1 := iblEpisodesResponse{}
	res, err := http.Get(url)
	if err != nil {
		returnError("Failed to get Episode Information")
	}
	body, _ := ioutil.ReadAll(res.Body)

	json.Unmarshal(body, &epResp1)

	if len(epResp1.Episodes) < 1 {
		return "nil", errors.New("Episode not found")
	}
	if len(epResp1.Episodes[0].Versions) < 1 {
		return "nil", errors.New("No Version available")
	}
	return epResp1.Episodes[0].Versions[0].ID, nil
}

func getExternalLinks(recordID string) []externalLink {
	configuration := getConfiguration()
	url := fmt.Sprintf(configuration.MusicURL, recordID)
	cert, err := tls.LoadX509KeyPair(configuration.CertFile, configuration.KeyFile)
	if err != nil {
		log.Fatal(err)
	}

	tlsConfig := &tls.Config{
		Certificates:       []tls.Certificate{cert},
		InsecureSkipVerify: true,
	}
	tlsConfig.BuildNameToCertificate()
	transport := &http.Transport{TLSClientConfig: tlsConfig}
	client := &http.Client{Transport: transport}

	musResp1 := musicResponse{}
	res, err := client.Get(url)
	if err != nil {
		returnError("Failed to get External Links")
	}
	body, _ := ioutil.ReadAll(res.Body)

	json.Unmarshal(body, &musResp1)
	return musResp1.Data.ExternalLinks
}

func getSpotifyData(trackIDs []spotify.ID) []spotifyTrackData {
	configuration := getConfiguration()
	config := &clientcredentials.Config{
		ClientID:     configuration.SpotifyClientID,
		ClientSecret: configuration.SpotifyClientSecret,
		TokenURL:     spotify.TokenURL,
	}

	token, err := config.Token(context.Background())
	if err != nil {
		returnError(fmt.Sprintf("couldn't get token: %v", err))
	}
	client := spotify.Authenticator{}.NewClient(token)

	var tracks []spotifyTrackData

	for _, trackID := range trackIDs {
		featurePointer, _ := client.GetAudioFeatures(trackID)
		analysisPointer, _ := client.GetAudioAnalysis(trackID)

		track := spotifyTrackData{
			Analysis: *analysisPointer,
			Features: *featurePointer[0],
		}

		tracks = append(tracks, track)
	}

	return tracks
}

func getMood(tracks []spotifyTrackData) mood {
	var moods []mood

	for _, track := range tracks {
		trackAnalysis := track.Analysis.Track
		trackFeatures := track.Features

		happiness := 5 * (trackFeatures.Valence - 0.5) * (trackFeatures.Danceability * trackFeatures.Energy * trackFeatures.Liveness)
		chillFactor := (float32(trackAnalysis.Tempo) / 120) * ((trackFeatures.Loudness + 30) / 30)

		moods = append(moods, mood{
			HappinessFactor: happiness,
			ChillFactor:     chillFactor,
		})
	}
	var totalHappiness, totalChillFactor float32 = 0, 0

	for _, mood := range moods {
		totalHappiness = totalHappiness + mood.HappinessFactor
		totalChillFactor = totalChillFactor + mood.ChillFactor
	}

	totalMoods := float32(len(moods))

	return mood{
		HappinessFactor: totalHappiness / totalMoods,
		ChillFactor:     totalChillFactor / totalMoods,
	}
}

func returnError(message string) {
	error := errorMessage{
		Error: message,
	}
	messageObject, _ := json.Marshal(error)
	fmt.Println(string(messageObject))
	os.Exit(1)
}

func main() {
	if len(os.Args) != 2 {
		returnError("Invalid number of arguments")
	}
	episodeID := os.Args[1]
	versionID, err := getVersionID(episodeID)
	if err != nil {
		returnError(err.Error())
	}
	recordIDs := getRecordIDs(versionID)

	var externalLinks []externalLink
	for _, recordID := range recordIDs {
		externalLinksForRecord := getExternalLinks(recordID)
		for _, externalLink := range externalLinksForRecord {
			externalLinks = append(externalLinks, externalLink)
		}
	}

	var spotifyIDs []spotify.ID

	for _, externalLink := range externalLinks {
		if externalLink.Type == "SPOTIFY" {
			spotifyIDSegments := strings.Split(externalLink.Value, ":")
			spotifyTrackID := spotifyIDSegments[2]
			spotifyIDs = append(spotifyIDs, spotify.ID(spotifyTrackID))
		}
	}

	spotifyFeatures := getSpotifyData(spotifyIDs)
	mood := getMood(spotifyFeatures)
	moodJSON, _ := json.Marshal(mood)

	if math.IsNaN(float64(mood.ChillFactor)) {
		returnError("No mood data available")
	}
	fmt.Println(string(moodJSON))
}
