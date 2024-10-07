package main

import (
    "bufio"
    "context"
    "crypto/tls"
    "encoding/base64"
    "flag"
    "fmt"
    "io/ioutil"
    "net/http"
    "os"
    "path/filepath"
    "strings"
    "sync"
    "time"
    "unicode/utf8"
    "golang.org/x/time/rate"
)

const (
    maxConcurrent = 10
    maxRetries    = 3
    timeout       = 5 * time.Second
)

var limiter = rate.NewLimiter(rate.Every(time.Second/10), maxConcurrent)

func main() {
    inputPath := flag.String("input", "domains.txt", "Path to the input file")
    outputDir := flag.String("output", ".", "Directory for output files")
    flag.Parse()

    fmt.Println("Starting domain processing...")

    inputFile, err := os.Open(*inputPath)
    if err != nil {
        fmt.Println("Error opening input file:", err)
        return
    }
    defer inputFile.Close()

    fmt.Printf("Successfully opened input file: %s\n", *inputPath)

    scanner := bufio.NewScanner(inputFile)
    var validDomains []string
    var wg sync.WaitGroup
    semaphore := make(chan struct{}, maxConcurrent)

    domainCount := 0
    for scanner.Scan() {
        domain := scanner.Text()
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

    writeToFile(filepath.Join(*outputDir, "vmess_list.txt"), uniqueVmess)
    writeToFile(filepath.Join(*outputDir, "trojan_list.txt"), uniqueTrojan)

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
            time.Sleep(time.Second * time.Duration(i+1))
            continue
        }
        defer resp.Body.Close()

        body, err := ioutil.ReadAll(resp.Body)
        if err != nil {
            fmt.Printf("Error reading body from %s: %v\n", domain, err)
            continue
        }

        // 将 body 转换为 UTF-8 编码
        bodyUTF8 := make([]rune, 0, len(body))
        for len(body) > 0 {
            r, size := utf8.DecodeRune(body)
            if r == utf8.RuneError {
                body = body[1:]
            } else {
                bodyUTF8 = append(bodyUTF8, r)
                body = body[size:]
            }
        }
        bodyString := string(bodyUTF8)

        if strings.Contains(bodyString, "Sansui233") && strings.Contains(bodyString, "目前共有抓取源") {
            return true
        }
        break
    }
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
