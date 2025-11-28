package main

import (
	"encoding/json"
	"fmt"
	"github.com/Masterminds/semver/v3"
	"github.com/go-git/go-git/v5"
	"github.com/joho/godotenv"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var (
	RepoDir       = "./repo"
	WebhookURL    string
	GitHubToken   string
	GitLabToken   string
	CodebergToken string
	repoURL       = "https://github.com/Aperture-OS/testing-blink-repo.git"
)

type Package struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Source  struct {
		URL string `json:"url"`
	} `json:"source"`
}

type Update struct {
	RepoName string
	PkgName  string
	Current  string
	Latest   string
	JsonPath string
	Warning  bool
}

/****************************************************/
// clean removes the repository folder if it exists
// used to reset the local repository state
/****************************************************/
func clean() {
	log.Println("[DEBUG] Cleaning up repository folder...")
	if err := os.RemoveAll(RepoDir); err != nil {
		log.Printf("[ERROR] Failed to remove repo folder: %v\n", err)
	} else {
		log.Println("[DEBUG] Repo folder removed successfully")
	}
}

/****************************************************/
// getRepo checks if the repository exists and is valid
// clones the repository if it does not exist or if it is corrupted
/****************************************************/
func getRepo() {
	log.Println("[DEBUG] Checking repository folder...")
	if _, err := os.Stat(RepoDir); err == nil {
		_, err := git.PlainOpen(RepoDir)
		if err != nil {
			log.Println("[DEBUG] Repo corrupted. Removing and recloning...")
			clean()
		} else {
			log.Printf("[DEBUG] Repository exists, exiting...")
			os.Exit(1)
		}
	}

	log.Println("[DEBUG] Cloning repository...")
	_, err := git.PlainClone(RepoDir, false, &git.CloneOptions{URL: repoURL})
	if err != nil {
		log.Fatalf("[ERROR] Failed to clone repo: %v", err)
	}
	log.Println("[DEBUG] Repository cloned successfully!")
}

/****************************************************/
// sendDiscord sends a message to a Discord webhook
// splits messages into chunks under 1900 characters to prevent truncation
/****************************************************/
func sendDiscord(content string) {
	const maxLen = 1900
	log.Printf("[DEBUG] Sending message to Discord, length=%d", len(content))

	for len(content) > 0 {
		chunk := content
		if len(chunk) > maxLen {
			chunk = chunk[:maxLen]
			lastNewline := strings.LastIndex(chunk, "\n")
			if lastNewline > 0 {
				chunk = chunk[:lastNewline]
			}
		}

		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			content = content[len(chunk):]
			content = strings.TrimLeft(content, "\n")
			continue
		}

		payload := map[string]string{"content": chunk}
		body, _ := json.Marshal(payload)

		resp, err := http.Post(WebhookURL, "application/json", strings.NewReader(string(body)))
		if err != nil {
			log.Printf("[ERROR] Discord POST error: %v", err)
		} else {
			log.Printf("[DEBUG] Discord POST success, status %d", resp.StatusCode)
			resp.Body.Close()
		}

		content = content[len(chunk):]
		content = strings.TrimLeft(content, "\n")
	}
}

/****************************************************/
// fetchLatestTag retrieves the latest semver tag from a repository API
// supports GitHub, GitLab, and Codeberg with optional authentication tokens
/****************************************************/
func fetchLatestTag(apiURL string, token string, provider string) (string, error) {
	req, _ := http.NewRequest("GET", apiURL, nil)
	if provider == "github" && token != "" {
		req.Header.Set("Authorization", "token "+token)
	}
	if provider == "gitlab" && token != "" {
		req.Header.Set("PRIVATE-TOKEN", token)
	}
	if provider == "codeberg" && token != "" {
		req.Header.Set("Authorization", "token "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var tags []struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return "", err
	}

	if len(tags) == 0 {
		return "", fmt.Errorf("no tags found")
	}

	versions := []*semver.Version{}
	for _, t := range tags {
		v, err := semver.NewVersion(t.Name)
		if err != nil {
			continue
		}
		versions = append(versions, v)
	}

	if len(versions) == 0 {
		return "", fmt.Errorf("no valid semver tags found")
	}

	sort.Sort(sort.Reverse(semver.Collection(versions)))
	return versions[0].String(), nil
}

/****************************************************/
// getGitHubLatestTag extracts repository info from a GitHub URL
// calls fetchLatestTag for GitHub API
/****************************************************/
func getGitHubLatestTag(url string) (string, error) {
	re := regexp.MustCompile(`github.com/([^/]+)/([^/]+)/`)
	matches := re.FindStringSubmatch(url)
	if len(matches) < 3 {
		return "", fmt.Errorf("invalid GitHub URL: %s", url)
	}
	user, repo := matches[1], matches[2]
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/tags", user, repo)
	return fetchLatestTag(apiURL, GitHubToken, "github")
}

/****************************************************/
// getGitLabLatestTag extracts project info from a GitLab URL
// calls fetchLatestTag for GitLab API
/****************************************************/
func getGitLabLatestTag(url string) (string, error) {
	re := regexp.MustCompile(`gitlab.com/([^/]+/[^/]+)/`)
	matches := re.FindStringSubmatch(url)
	if len(matches) < 2 {
		return "", fmt.Errorf("invalid GitLab URL: %s", url)
	}
	project := strings.ReplaceAll(matches[1], "/", "%2F")
	apiURL := fmt.Sprintf("https://gitlab.com/api/v4/projects/%s/repository/tags", project)
	return fetchLatestTag(apiURL, GitLabToken, "gitlab")
}

/****************************************************/
// getCodebergLatestTag extracts repository info from a Codeberg URL
// calls fetchLatestTag for Codeberg API
/****************************************************/
func getCodebergLatestTag(url string) (string, error) {
	re := regexp.MustCompile(`codeberg.org/([^/]+)/([^/]+)/`)
	matches := re.FindStringSubmatch(url)
	if len(matches) < 3 {
		return "", fmt.Errorf("invalid Codeberg URL: %s", url)
	}
	user, repo := matches[1], matches[2]
	apiURL := fmt.Sprintf("https://codeberg.org/api/v1/repos/%s/%s/tags", user, repo)
	return fetchLatestTag(apiURL, CodebergToken, "codeberg")
}

/****************************************************/
// getVersionFromURL extracts a version string from a URL using regex
/****************************************************/
func getVersionFromURL(url string) string {
	re := regexp.MustCompile(`(\d+\.\d+\.\d+)`)
	v := re.FindString(url)
	log.Printf("[DEBUG] Extracted version from URL (%s): %s", url, v)
	return v
}

/****************************************************/
// parseJSONFile reads a JSON file and unmarshals it into a Package struct
/****************************************************/
func parseJSONFile(path string) (*Package, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var pkg Package
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, err
	}
	return &pkg, nil
}

// main function is the core function, self explanatory
func main() {
	// Load environment
	_ = godotenv.Load()
	WebhookURL = os.Getenv("WEBHOOK_URL")
	GitHubToken = os.Getenv("GITHUB_TOKEN")
	GitLabToken = os.Getenv("GITLAB_TOKEN")
	CodebergToken = os.Getenv("CODEBERG_TOKEN")

	if WebhookURL == "" {
		log.Fatal("[ERROR] WEBHOOK_URL not set")
	}

	// Clone repo
	getRepo()

	var updates []Update
	filepath.Walk(RepoDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(info.Name(), ".json") {
			return nil
		}

		pkg, err := parseJSONFile(path)
		if err != nil || pkg.Source.URL == "" {
			return nil
		}

		var latest string
		switch {
		case strings.Contains(pkg.Source.URL, "github.com"):
			latest, err = getGitHubLatestTag(pkg.Source.URL)
		case strings.Contains(pkg.Source.URL, "gitlab.com"):
			latest, err = getGitLabLatestTag(pkg.Source.URL)
		case strings.Contains(pkg.Source.URL, "codeberg.org"):
			latest, err = getCodebergLatestTag(pkg.Source.URL)
		default:
			latest = getVersionFromURL(pkg.Source.URL)
		}

		if err != nil {
			log.Printf("[ERROR] Failed to get latest version for %s: %v", pkg.Name, err)
			return nil
		}

		currentVer, err1 := semver.NewVersion(pkg.Version)
		latestVer, err2 := semver.NewVersion(latest)
		warning := false
		if err1 == nil && err2 == nil && currentVer.GreaterThan(latestVer) {
			warning = true
			log.Printf("[WARN] Package retroceded: %s %s → %s", pkg.Name, pkg.Version, latest)
		} else if err1 != nil || err2 != nil || currentVer.Equal(latestVer) {
			return nil
		}

		relPath, _ := filepath.Rel(RepoDir, filepath.Dir(path))
		parts := strings.Split(relPath, string(os.PathSeparator))
		repoName := parts[0]

		updates = append(updates, Update{
			RepoName: repoName,
			PkgName:  pkg.Name,
			Current:  pkg.Version,
			Latest:   latest,
			JsonPath: path,
			Warning:  warning,
		})

		// Small delay to respect rate limits (~1 req/sec)
		time.Sleep(800 * time.Millisecond)
		return nil
	})

	// Send header
	date := time.Now().Format("02 January 2006")
	sendDiscord(fmt.Sprintf("||<@&1417420496655482930>||\n# Repository Checklist [%s]", date))

	if len(updates) == 0 {
		sendDiscord("No new versions found.")
		clean()
		return
	}

	for _, u := range updates {
		msg := ""
		if u.Warning {
			msg += "# <:warn:1428846936219324476> - ! PACKAGE RETROCEDED ! "
		}
		msg += fmt.Sprintf("```- %s/%s %s → %s```", u.RepoName, u.PkgName, u.Current, u.Latest)
		sendDiscord(msg)
	}

	clean()
	log.Println("[DEBUG] Done.")
}
