package common

import (
	"io"
	"log"
	"net/http"
	"os"
)

func StatusOK(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
}

func DownloadURLToFile(URL string, fileName string) {
	fd, err := os.Create(fileName)
	if err != nil {
		log.Fatal(err)
	}

	resp, err := http.Get(fileName)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	_, err := io.Copy(fd, resp.Body)
	if err != nil {
		log.Fatal(err)
	}
	defer fd.Close()

}
