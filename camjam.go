package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"

	"github.com/gorilla/handlers"
)

type Video struct {
	Url  string
	Time time.Time
}

var videoCache = make([]Video, 0)
var lastUserVideoListRequest = time.Now()

func init() {
	go runUpdateWorker()
}

func runUpdateWorker() {
	t := time.NewTicker(time.Second * 10)
	for {
		newCache, err := fetchLatestVideos()
		if err == nil {
			videoCache = newCache
		}
		<-t.C
		if time.Since(lastUserVideoListRequest) > time.Second*30 {
			time.Sleep(time.Second * 60)
		}
	}
}

func fetchLatestVideos() ([]Video, error) {
	s3svc := s3.New(session.New(), &aws.Config{Region: aws.String("eu-west-1"), Credentials: credentials.AnonymousCredentials})
	params := &s3.ListObjectsV2Input{
		Bucket: aws.String("jamcams.tfl.gov.uk"),
	}

	var videos []Video
	err := s3svc.ListObjectsV2Pages(params, func(page *s3.ListObjectsV2Output, lastPage bool) bool {
		for _, key := range page.Contents {
			if strings.HasSuffix(*key.Key, ".mp4") {
				baseUrl := "https://s3-eu-west-1.amazonaws.com/jamcams.tfl.gov.uk/%s"
				url := fmt.Sprintf(baseUrl, *key.Key)
				if *key.Size > 20000 {
					videos = append(videos, Video{url, *key.LastModified})
				}
			}
		}
		return true
	})

	if err != nil {
		log.Printf("Failed to fetch videos: %s", err)
		return videos, err
	}

	sort.Slice(videos[:], func(i, j int) bool {
		return videos[i].Time.After(videos[j].Time)
	})

	return videos, nil
}

func handleLatestVideos(w http.ResponseWriter, r *http.Request) {
	log.Printf("%s %s %s\n", r.RemoteAddr, r.Method, r.URL)
	lastUserVideoListRequest = time.Now()

	js, err := json.Marshal(videoCache[:50])
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(js)
}

//go:embed root.html
var rootHtml []byte
var rootHtmlContentType = http.DetectContentType(rootHtml)

func handleRoot(w http.ResponseWriter, r *http.Request) {
	log.Printf("%s %s %s\n", r.RemoteAddr, r.Method, r.URL)

	r.Header.Add("Content-Type", rootHtmlContentType)
	w.Write(rootHtml)
}

func main() {
	portApi, ok := os.LookupEnv("PORT")
	if !ok || len(portApi) == 0 {
		log.Panic("You must specify a :PORT")
	}

	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	mux := http.NewServeMux()

	mux.HandleFunc("/latest_videos", handleLatestVideos)
	mux.HandleFunc("/", handleRoot)

	s := &http.Server{
		Addr:           fmt.Sprintf("%s", portApi),
		Handler:        handlers.CompressHandler(mux),
		ReadTimeout:    60 * time.Second,
		WriteTimeout:   60 * time.Second,
		MaxHeaderBytes: 1 << 11,
	}
	s.ListenAndServe()
}
