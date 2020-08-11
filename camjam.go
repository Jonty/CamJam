package main

import (
	"encoding/json"
	"fmt"
	"html/template"
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
	"github.com/gorilla/mux"
)

type Video struct {
	Url  string
	Time time.Time
}

var videoCache = make([]Video, 0)

func init() {
	go runUpdateWorker()
}

func runUpdateWorker() {
	for {
		videoCache = fetchLatestVideos()
		time.Sleep(10 * time.Second)
	}
}

func fetchLatestVideos() []Video {
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
				videos = append(videos, Video{url, *key.LastModified})
			}
		}
		return true
	})

	if err != nil {
		log.Fatal("Failed to fetch videos: %s", err)
	}

	sort.Slice(videos[:], func(i, j int) bool {
		return videos[i].Time.After(videos[j].Time)
	})

	return videos
}

func handleLatestVideos(w http.ResponseWriter, r *http.Request) {
	log.Printf("%s %s %s\n", r.RemoteAddr, r.Method, r.URL)

	js, err := json.Marshal(videoCache[:50])
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(js)
}

func handleRoot(w http.ResponseWriter, r *http.Request) {
	log.Printf("%s %s %s\n", r.RemoteAddr, r.Method, r.URL)

	t, err := template.ParseFiles("root.html")
	if err != nil {
		log.Print("Template parsing error: ", err)
	}

	err = t.Execute(w, nil)
	if err != nil {
		log.Print("Template executing error: ", err)
	}
}

func main() {
	portApi, ok := os.LookupEnv("PORT")
	if !ok || len(portApi) == 0 {
		log.Panic("You must specify a :PORT")
	}

	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	router := mux.NewRouter().StrictSlash(true)
	router.HandleFunc("/latest_videos", handleLatestVideos).Methods("GET")
	router.HandleFunc("/", handleRoot).Methods("GET")

	s := &http.Server{
		Addr:           fmt.Sprintf("%s", portApi),
		Handler:        handlers.CompressHandler(router),
		ReadTimeout:    60 * time.Second,
		WriteTimeout:   60 * time.Second,
		MaxHeaderBytes: 1 << 11,
	}
	s.ListenAndServe()
}
