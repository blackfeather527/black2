package main

import (
    "bufio"
    "crypto/tls"
    "encoding/base64"
    "fmt"
    "io/ioutil"
    "net/http"
    "os"
    "strings"
    "sync"
    "time"
)

func main() {
    inputFile, err := os.Open("/tmp/fofa_output/domains.txt")
    if err != nil {
        fmt.Println("Error opening input file:", err)
        return
    }
    defer inputFile.Close()

    scanner := bufio.NewScanner(inputFile)
    var validDomains []string
    var wg sync.WaitGroup

    for scanner.Scan() {
        domain := scanner.Text()
        wg.Add(1)
        go func(d string) {
            defer wg.Done()
            if checkDomain(d) {
                validDomains = append(validDomains, d)
            }
        }(domain)
    }

    wg.Wait()

    uniqueDomains := removeDuplicates(validDomains)

    vmessList := []string{}
    trojanList := []string{}

    for _, domain := range uniqueDomains {
        vmess := getSubContent(domain, "/vmess/sub")
        trojan := getSubContent(domain, "/trojan/sub")

        if vmess != "" {
            vmessList = append(vmessList, decodeAndFilter(vmess, "vmess")...)
        }
        if trojan != "" {
            trojanList = append(trojanList, decodeAndFilter(trojan, "trojan")...)
        }
    }

    uniqueVmess := removeDuplicates(vmessList)
    uniqueTrojan := removeDuplicates(trojanList)

    writeToFile("vmess_list.txt", uniqueVmess)
    writeToFile("trojan_list.txt", uniqueTrojan)
}

func checkDomain(domain string) bool {
    client := &http.Client{
        Timeout: 10 * time.Second,
        Transport: &http.Transport{
            TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
        },
    }

    for _, scheme := range []string{"https", "http"} {
        resp, err := client.Get(fmt.Sprintf("%s://%s", scheme, domain))
        if err != nil {
            continue
        }
        defer resp.Body.Close()

        body, _ := ioutil.ReadAll(resp.Body)
        if strings.Contains(string(body), "Sansui233") {
            return true
        }
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
        Timeout: 10 * time.Second,
        Transport: &http.Transport{
            TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
        },
    }

    for _, scheme := range []string{"https", "http"} {
        resp, err := client.Get(fmt.Sprintf("%s://%s%s", scheme, domain, path))
        if err != nil {
            continue
        }
        defer resp.Body.Close()

        body, _ := ioutil.ReadAll(resp.Body)
        return string(body)
    }
    return ""
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
}
