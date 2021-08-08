package main

import (
	"embed"
	_ "embed"
	"flag"
	"gopkg.in/yaml.v2"
	"html/template"
	"io/fs"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
)

var (
	Version = "dev"

	photosDirFlag  = flag.String("photos-dir", "photos", "")
	outputDirFlag  = flag.String("output-dir", "dist", "")
	configFileFlag = flag.String("config-file", "config.yaml", "")

	//go:embed res/*
	resDirectory embed.FS
)

type Config struct {
	Title string `yaml:"title"`

	Photos []map[string]interface{}
}

func main() {
	flag.Parse()

	log.Printf("running go-gallery %s", Version)

	config, err := readConfig()
	if err != nil {
		log.Fatalf("error while reading config: %s", err)
	}

	// Create dist folder
	if err := os.Mkdir(*outputDirFlag, 0750); err != nil && !os.IsExist(err) {
		log.Fatalf("error while creating %s/ folder: %s", *outputDirFlag, err)
	}

	photos, err := processImages(*photosDirFlag, *outputDirFlag)
	if err != nil {
		log.Fatalf("error while processing images: %s", err)
	}
	config.Photos = photos

	// Generate the index.html
	if err := generateIndex(config, *outputDirFlag); err != nil {
		log.Fatalf("error while generating index.html: %s", err)
	}

	// Copy the index.css
	if err := copyCssStyle(*outputDirFlag); err != nil {
		log.Fatalf("error while copying index.css: %s", err)
	}
}

func readConfig() (Config, error) {
	f, err := os.Open(*configFileFlag)
	if err != nil {
		return Config{}, err
	}
	defer f.Close()

	var config Config
	if err := yaml.NewDecoder(f).Decode(&config); err != nil {
		return Config{}, err
	}

	return config, nil
}

func generateIndex(config Config, distDirectory string) error {
	t, err := template.New("index.gohtml").ParseFS(resDirectory, "res/index.gohtml")
	if err != nil {
		return err
	}

	f, err := os.OpenFile(filepath.Join(distDirectory, "index.html"), os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0640)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := t.ExecuteTemplate(f, "index.gohtml", config); err != nil {
		return err
	}

	return nil
}

func copyCssStyle(distDirectory string) error {
	style, err := resDirectory.ReadFile("res/index.css")
	if err != nil {
		return err
	}
	if err := ioutil.WriteFile(filepath.Join(distDirectory, "index.css"), style, 0640); err != nil {
		return err
	}

	return nil
}

func processImages(photosDir, outputDir string) ([]map[string]interface{}, error) {
	if err := os.MkdirAll(filepath.Join(outputDir, "photos", "thumbnails"), 0750); err != nil {
		return nil, err
	}

	var photos []map[string]interface{}

	if err := filepath.Walk(photosDir, func(path string, info fs.FileInfo, err error) error {
		if info.IsDir() {
			return nil
		}

		log.Printf("processing %s", path)

		imgBytes, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		// TODO generate thumbnails

		// Copy the image
		if err := ioutil.WriteFile(filepath.Join(outputDir, "photos", info.Name()), imgBytes, 0640); err != nil {
			return err
		}

		photos = append(photos, map[string]interface{}{
			"ImgPath": filepath.Join("photos", info.Name()),
		})

		return nil
	}); err != nil {
		return nil, err
	}

	return photos, nil
}
