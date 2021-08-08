package main

import (
	_ "embed"
	"gopkg.in/yaml.v2"
	"html/template"
	"log"
	"os"
	"path/filepath"
)

var (
	Version = "dev"

	distFolder = "dist"
	configFile = "config.yaml"

	//go:embed index.gohtml
	indexTemplate string
)

type Config struct {
	Title string `yaml:"title"`
}

func main() {
	log.Printf("running go-gallery %s", Version)

	config, err := readConfig()
	if err != nil {
		log.Fatalf("error while reading config: %s", err)
	}

	// Create dist folder
	if err := os.Mkdir(distFolder, 0750); err != nil {
		log.Fatalf("error while creating %s/ folder: %s", distFolder, err)
	}

	t, err := template.New("index").Parse(indexTemplate)
	if err != nil {
		log.Fatalf("error while parsing template: %s", err)
	}

	f, err := os.OpenFile(filepath.Join(distFolder, "index.html"), os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0640)
	if err != nil {
		log.Fatalf("error while opening index file: %s", err)
	}
	defer f.Close()

	if err := t.ExecuteTemplate(f, "index", config); err != nil {
		log.Fatalf("error while executing template: %s", err)
	}
}

func readConfig() (Config, error) {
	f, err := os.Open(configFile)
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
