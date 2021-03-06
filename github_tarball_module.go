package main

import "crypto/sha1"
import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path"
	"strings"
)

type GithubTarballModule struct {
	name        string
	repoName    string
	version     string
	cacheFolder string
	envRoot     string
	installPath string
	processed   func()
}

type GHModuleReleases []struct {
	Name        string
	Tarball_url string
}

func (m *GithubTarballModule) Name() string {
	return m.name
}

func (m *GithubTarballModule) SetEnvRoot(s string) {
	m.envRoot = s
}

func (m *GithubTarballModule) Processed() {
	m.processed()
}

func (m *GithubTarballModule) TargetFolder() string {
	if m.envRoot == "" {
		log.Fatal("Environment root not defined")
	}

	splitPath := strings.FieldsFunc(m.name, func(r rune) bool {
		return r == '/' || r == '-'
	})
	folderName := splitPath[len(splitPath)-1]
	if folderName == "" {
		log.Fatal("Oups")
	}

	if m.installPath != "" {
		return path.Join(m.envRoot, m.installPath, folderName)
	}

	return path.Join(m.envRoot, "modules", folderName)
}

func (m *GithubTarballModule) SetCacheFolder(cacheFolder string) {
	m.cacheFolder = cacheFolder
}

func (m *GithubTarballModule) Hash() string {
	hasher := sha1.New()
	hasher.Write([]byte(m.name))
	return base64.URLEncoding.EncodeToString(hasher.Sum(nil))
}

func (m *GithubTarballModule) IsUpToDate() bool {
	_, err := os.Stat(m.TargetFolder())
	if err != nil {
		return false
	} else if m.version == "" {
		// Module is present and no version specified...
		return true
	}

	versionFile := path.Join(m.TargetFolder(), ".version")
	version, err := ioutil.ReadFile(versionFile)
	if err != nil {
		// TODO error handling
		fmt.Println("Error opening version file :" + err.Error())
		return false
	}
	v := string(version)

	return v == m.version
}

func (m *GithubTarballModule) downloadToCache(r io.Reader) error {
	if err := os.MkdirAll(path.Join(m.cacheFolder), 0755); err != nil {
		return err
	}

	out, err := os.Create(path.Join(m.cacheFolder, m.version+".tar.gz"))
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, r)

	return err
}

func (m *GithubTarballModule) downloadURL() (string, error) {
	ghAPIRoot := "https://api.github.com"

	url := ghAPIRoot + "/repos/" + m.repoName + "/tags"

	resp, err := http.Get(url)
	if err != nil {
		return "", &DownloadError{err, true}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", &DownloadError{fmt.Errorf("failed retrieving URL - %s", resp.Status), true}
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", &DownloadError{err, true}
	}

	var gr GHModuleReleases
	if err = json.Unmarshal(body, &gr); err != nil {
		return "", err
	}

	index := 0
	if m.version != "" {
		versionFound := false
		for i, result := range gr {
			if m.version == result.Name {
				versionFound = true
				index = i
				break
			}
		}
		if !versionFound {
			return "", &DownloadError{fmt.Errorf("Could not find version %s for module %s", m.version, m.Name()), false}
		}
	} else {
		m.version = gr[0].Name
	}

	return gr[index].Tarball_url, nil
}

func (m *GithubTarballModule) Download() DownloadError {
	var err error
	var url string

	if url, err = m.downloadURL(); err != nil {
		return DownloadError{err, true}
	}

	if _, err = os.Stat(path.Join(m.cacheFolder, m.version+".tar.gz")); err != nil {
		forgeArchive, err := http.Get(url)
		if err != nil {
			return DownloadError{fmt.Errorf("Failed retrieving %s", url), true}
		}
		defer forgeArchive.Body.Close()

		m.downloadToCache(forgeArchive.Body)
	}

	r, err := os.Open(path.Join(m.cacheFolder, m.version+".tar.gz"))
	if err != nil {
		return DownloadError{err, false}
	}

	defer r.Close()

	if err = extract(r, m.TargetFolder()); err != nil {
		return DownloadError{err, false}
	}

	versionFile := path.Join(m.TargetFolder(), ".version")
	r, err = os.OpenFile(versionFile, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return DownloadError{fmt.Errorf("Failed creating file %s", versionFile), false}
	}
	defer r.Close()
	r.WriteString(m.version)

	return DownloadError{nil, false}
}
