package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

const baseURL = "https://oral.planez.co"

type ImageCache struct {
	data map[string]struct{}
	mu   sync.RWMutex
}

func (c *ImageCache) Add(value string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.data[value] = struct{}{}
}

func (c *ImageCache) Values() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	values := make([]string, 0, len(c.data))
	for key := range c.data {
		values = append(values, key)
	}

	return values
}

type Question struct {
	Answer      string  `json:"answer"`
	Certificate string  `json:"certificate"`
	CreatedDate int     `json:"createdDate"`
	ImageFile   *string `json:"imageFile"`
	Question    string  `json:"question"`
	QuestionID  int     `json:"questionId"`
	Type        string  `json:"type"`
}

func scrape(client *http.Client, imgCache *ImageCache, questionID int) (Question, error) {
	res, err := client.Get(baseURL + "/api/question/" + strconv.Itoa(questionID))
	if err != nil {
		return Question{}, fmt.Errorf("failed to retrieve question %d: %v", questionID, err)
	}

	if res.StatusCode != http.StatusOK {
		return Question{}, fmt.Errorf("failed to retrieve question %d: received status %d", questionID, res.StatusCode)
	}

	defer res.Body.Close()

	var data Question
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return Question{}, fmt.Errorf("failed to retrieve question %d: failed to decode response body: %v", questionID, err)
	}

	if data.ImageFile != nil {
		imgCache.Add(*data.ImageFile)
	}

	return data, nil
}

func write(data []Question) error {
	path := filepath.Join("data", "questions.json")

	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create %s: %v", path, err)
	}

	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(data); err != nil {
		return fmt.Errorf("failed to write to %s: %v", path, err)
	}

	return nil
}

func readImages(cache *ImageCache) {
	for _, image := range cache.Values() {
		readImage(image)
	}
}

func readImage(image string) {
	res, err := http.Get(baseURL + "/images/" + image)
	if err != nil {
		log.Printf("Failed to download image %s: %v\n", image, err)
		return
	}

	if res.StatusCode != http.StatusOK {
		log.Printf("Failed to retrieve image %s: status %d\n", image, res.StatusCode)
		return
	}

	defer res.Body.Close()

	destPath := filepath.Join("data", "images", image)
	file, err := os.Create(destPath)
	if err != nil {
		log.Printf("Failed to create %s: %v\n", destPath, err)
		return
	}

	defer file.Close()

	if _, err := io.Copy(file, res.Body); err != nil {
		log.Printf("Failed to write %s: %v\n", destPath, err)
		return
	}

	log.Println("Wrote image", destPath)
}

func main() {
	if err := os.RemoveAll("data"); err != nil {
		log.Fatalln("Failed to clear 'data' directory:", err)
	}

	if err := os.Mkdir("data", 0755); err != nil {
		log.Fatalln("Failed to create 'data' directory:", err)
	}

	if err := os.Mkdir("data/images", 0755); err != nil {
		log.Fatalln("Failed to create 'data/images' directory:", err)
	}

	imgCache := &ImageCache{data: make(map[string]struct{})}

	var data []Question
	for i := 1000; i <= 1305; i++ {
		q, err := scrape(http.DefaultClient, imgCache, i)
		if err != nil {
			log.Printf("Error scraping question %d: %v\n", i, err)
			continue
		}

		data = append(data, q)
		log.Println("Successfully scraped question", i)
	}

	if err := write(data); err != nil {
		log.Fatalln("Failed to write question data:", err)
	}

	readImages(imgCache)
}
