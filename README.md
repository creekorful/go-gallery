# go-gallery

Generate a portfolio static website for your photos. 
A demo is available [here](https://photos.creekorful.org).

```
$ ./go-gallery -photos-dir ~/Photos -output-dir /var/www/photos.example.org -config-file config.yaml
```

Where `-photos-dir` is the directory where the images are stored (*.jpg, *.jpeg), `-output-dir` is the directory where
the static website will be copied and `-config-file` is the path to the configuration file.

## The configuration file

```yaml
title: 'My Photos'
``` 