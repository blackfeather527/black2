package main

import (
    "bufio"
    "context"
    "crypto/tls"
    "encoding/base64"
    "fmt"
    "io/ioutil"
    "net/http"
    "os"
    "strings"
    "sync"
    "time"
    "golang.org/x/time/rate"
)

const (
    maxConcurrent = 10
    maxRetries    = 3
    timeout       = 30 * time.Second
)

var limiter = rate.NewLimiter(rate.Every(time.Second/10), maxConcurrent)

func main() {
    fmt.Println("Starting domain processing...")

    inputFile, err := os.Open("/tmp/fofa_output/domains.txt")
    if err != nil {
        fmt.Println("Error opening input file:", err)
        return
    }
    defer inputFile.Close()

    fmt.Println("Successfully opened input file.")

    scanner := bufio.NewScanner(inputFile)
    var validDomains []string
    var wg sync.WaitGroup
    semaphore := make(chan struct{}, maxConcurrent)

    domainCount := 0
    for scanner.Scan() {
        domain := scanner.Text()
        fmt.Printf("Read domain: %s\n", domain)
        domainCount++
        wg.Add(1)
        go func(d string) {
            defer wg.Done()
            semaphore <- struct{}{}
            defer func() { <-semaphore }()
            if err := limiter.Wait(context.Background()); err != nil {
                fmt.Printf("Rate limit error: %v\n", err)
                return
            }
            if checkDomain(d) {
                validDomains = append(validDomains, d)
                fmt.Printf("Valid domain found: %s\n", d)
            }
        }(domain)
    }

    fmt.Printf("Total domains to check: %d\n", domainCount)

    wg.Wait()

    fmt.Printf("Number of valid domains: %d\n", len(validDomains))

    uniqueDomains := removeDuplicates(validDomains)
    fmt.Printf("Number of unique valid domains: %d\n", len(uniqueDomains))

    vmessList := []string{}
    trojanList := []string{}

    for _, domain := range uniqueDomains {
        fmt.Printf("Processing domain: %s\n", domain)
        vmess := getSubContent(domain, "/vmess/sub")
        trojan := getSubContent(domain, "/trojan/sub")

        if vmess != "" {
            decodedVmess := decodeAndFilter(vmess, "vmess")
            vmessList = append(vmessList, decodedVmess...)
            fmt.Printf("Found %d vmess configs for %s\n", len(decodedVmess), domain)
        }
        if trojan != "" {
            decodedTrojan := decodeAndFilter(trojan, "trojan")
            trojanList = append(trojanList, decodedTrojan...)
            fmt.Printf("Found %d trojan configs for %s\n", len(decodedTrojan), domain)
        }
    }

    uniqueVmess := removeDuplicates(vmessList)
    uniqueTrojan := removeDuplicates(trojanList)

    fmt.Printf("Total unique vmess configs: %d\n", len(uniqueVmess))
    fmt.Printf("Total unique trojan configs: %d\n", len(uniqueTrojan))

    writeToFile("vmess_list.txt", uniqueVmess)
    writeToFile("trojan_list.txt", uniqueTrojan)

    fmt.Println("Processing completed.")
}

func checkDomain(domain string) bool {
    client := &http.Client{
        Timeout: timeout,
        Transport: &http.Transport{
            TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
        },
    }

    for i := 0; i < maxRetries; i++ {
        resp, err := client.Get(domain)
        if err != nil {
            fmt.Printf("Error checking %s (attempt %d): %v\n", domain, i+1, err)
            time.Sleep(time.Second * time.Duration(i+1))
            continue
        }
        defer resp.Body.Close()

        body, _ := ioutil.ReadAll(resp.Body)
        if strings.Contains(string(body), "Sansui233") {
            fmt.Printf("Domain %s is valid\n", domain)
            return true
        }
        break
    }
    fmt.Printf("Domain %s is invalid\n", domain)
    return false
}

func removeDuplicates(list []string) []string {
    seen := make(map[string]bool)
    result := []string{}
    for _, item := range list {
        if _, ok := seen[item]; !ok {
            seen[item] = true
            result = append(result, item)
        }
    }
    return result
}

func getSubContent(domain, path string) string {
    client := &http.Client{
        Timeout: timeout,
        Transport: &http.Transport{
            TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
        },
    }

    url := domain + path
    resp, err := client.Get(url)
    if err != nil {
        fmt.Printf("Error getting content from %s: %v\n", url, err)
        return ""
    }
    defer resp.Body.Close()

    body, _ := ioutil.ReadAll(resp.Body)
    fmt.Printf("Successfully got content from %s\n", url)
    return string(body)
}

func decodeAndFilter(content, prefix string) []string {
    decoded, err := base64.StdEncoding.DecodeString(content)
    if err != nil {
        fmt.Println("Error decoding content:", err)
        return []string{}
    }
    lines := strings.Split(string(decoded), "\n")
    result := []string{}
    for _, line := range lines {
        if strings.HasPrefix(line, prefix) {
            result = append(result, line)
        }
    }
    return result
}

func writeToFile(filename string, content []string) {
    file, err := os.Create(filename)
    if err != nil {
        fmt.Println("Error creating file:", err)
        return
    }
    defer file.Close()

    writer := bufio.NewWriter(file)
    for _, line := range content {
        fmt.Fprintln(writer, line)
    }
    writer.Flush()
    fmt.Printf("Successfully wrote %d lines to %s\n", len(content), filename)
}
