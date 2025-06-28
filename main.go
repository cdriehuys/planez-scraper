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

type QuestionData struct {
	id   int
	data Question
}

func scrape(client *http.Client, questionID int, results chan QuestionData, errors chan error) {
	res, err := client.Get(baseURL + "/api/question/" + strconv.Itoa(questionID))
	if err != nil {
		errors <- fmt.Errorf("failed to retrieve question %d: %v", questionID, err)
	}

	if res.StatusCode != http.StatusOK {
		errors <- fmt.Errorf("failed to retrieve question %d: received status %d", questionID, res.StatusCode)
		return
	}

	defer res.Body.Close()

	var data Question
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		errors <- fmt.Errorf("failed to retrieve question %d: failed to decode response body: %v", questionID, err)
		return
	}

	results <- QuestionData{questionID, data}
}

func write(data QuestionData) error {
	path := filepath.Join("data", strconv.Itoa(data.id)+".json")

	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create %s: %v", path, err)
	}

	defer file.Close()

	if err := json.NewEncoder(file).Encode(data.data); err != nil {
		return fmt.Errorf("failed to write to %s: %v", path, err)
	}

	return nil
}

func startWriter(wg *sync.WaitGroup, imageCache *ImageCache, results chan QuestionData) {
	defer wg.Done()

	for result := range results {
		if result.data.ImageFile != nil {
			imageCache.Add(*result.data.ImageFile)
		}

		if err := write(result); err != nil {
			log.Println("Failed to write result:", err)
		} else {
			log.Printf("Wrote data for question %d\n", result.id)
		}
	}
}

func startScraper(wg *sync.WaitGroup, ids chan int, results chan QuestionData, errors chan error) {
	defer wg.Done()

	client := http.Client{}

	for id := range ids {
		scrape(&client, id, results, errors)
	}
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
	workers := 4

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

	ids := make(chan int)
	results := make(chan QuestionData)
	errs := make(chan error)

	wg := &sync.WaitGroup{}

	wg.Add(1)
	go startWriter(wg, imgCache, results)

	wg.Add(1)
	go func() {
		defer wg.Done()

		for err := range errs {
			log.Println("Error:", err)
		}
	}()

	scraperWg := &sync.WaitGroup{}
	for range workers {
		scraperWg.Add(1)
		go startScraper(scraperWg, ids, results, errs)
	}

	for i := 1000; i <= 1305; i++ {
		ids <- i
	}

	close(ids)

	scraperWg.Wait()

	close(results)
	close(errs)

	wg.Wait()

	readImages(imgCache)
}
