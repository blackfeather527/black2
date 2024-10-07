package main

import (
    "bufio"
    "context"
    "crypto/tls"
    "database/sql"
    "flag"
    "fmt"
    "io/ioutil"
    "net/http"
    "net/url"
    "os"
    "strings"
    "sync"
    "time"
    "unicode/utf8"

    "golang.org/x/time/rate"
    _ "github.com/mattn/go-sqlite3"
)

const (
    maxConcurrent = 10
    maxRetries    = 3
    timeout       = 5 * time.Second
)

var (
    inputPath        *string
    outputDir        *string
    failureThreshold *int
    silentDays       *int
    dbPath           *string
)

var limiter = rate.NewLimiter(rate.Every(time.Second/10), maxConcurrent)

type Domain struct {
    Protocol string
    Host     string
    Port     string
}

func init() {
    // 定义命令行参数
    inputPath = flag.String("input", "domains.txt", "输入文件路径")
    outputDir = flag.String("output", ".", "输出文件目录")
    failureThreshold = flag.Int("failures", 5, "检测失败次数阈值")
    silentDays = flag.Int("silent", 7, "检测静默天数")
    dbPath = flag.String("db", "domains.db", "SQLite 数据库路径")
}

func main() {
    // 解析命令行参数
    flag.Parse()

    // 打印命令行参数以防止未使用警告
    fmt.Printf("输入文件路径: %s\n", *inputPath)
    fmt.Printf("输出文件目录: %s\n", *outputDir)
    fmt.Printf("检测失败次数阈值: %d\n", *failureThreshold)
    fmt.Printf("检测静默天数: %d\n", *silentDays)
    fmt.Printf("SQLite 数据库路径: %s\n", *dbPath)

    // 调用函数处理输入文件
    domains := processDomainFile(*inputPath)

    // 检查域名的有效性
    validDomains := checkDomains(domains)

    fmt.Printf("最终有效域名数量: %d\n", len(validDomains))

    // 这里可以继续处理 validDomains，例如写入文件等
}

func processDomainFile(inputPath string) []Domain {
    file, err := os.Open(inputPath)
    if err != nil {
        fmt.Printf("打开文件失败: %v\n", err)
        return nil
    }
    defer file.Close()

    domainMap := make(map[string]Domain)
    scanner := bufio.NewScanner(file)
    lineCount := 0
    validCount := 0

    for scanner.Scan() {
        lineCount++
        line := strings.TrimSpace(scanner.Text())
        if line == "" {
            continue
        }

        domain, ok := parseDomain(line)
        if !ok {
            fmt.Printf("第 %d 行格式不正确: %s\n", lineCount, line)
            continue
        }

        key := domain.Host
        if domain.Port != "80" && domain.Port != "443" {
            key += ":" + domain.Port
        }
        
        if _, exists := domainMap[key]; !exists {
            domainMap[key] = domain
            validCount++
            fmt.Printf("有效域名: %s://%s:%s\n", domain.Protocol, domain.Host, domain.Port)
        }
    }

    if err := scanner.Err(); err != nil {
        fmt.Printf("读取文件时发生错误: %v\n", err)
    }

    domains := make([]Domain, 0, len(domainMap))
    for _, domain := range domainMap {
        domains = append(domains, domain)
    }

    fmt.Printf("总行数: %d, 唯一有效域名数: %d\n", lineCount, validCount)
    return domains
}

func parseDomain(line string) (Domain, bool) {
    u, err := url.Parse(line)
    if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
        return Domain{}, false
    }

    domain := Domain{
        Protocol: u.Scheme,
        Host:     u.Hostname(),
        Port:     u.Port(),
    }

    if domain.Port == "" {
        if domain.Protocol == "http" {
            domain.Port = "80"
        } else {
            domain.Port = "443"
        }
    }

    return domain, true
}

func checkDomains(domains []Domain) []Domain {
    db, err := initDB()
    if err != nil {
        fmt.Printf("初始化数据库失败: %v\n", err)
        return nil
    }
    defer db.Close()

    var validDomains []Domain
    var wg sync.WaitGroup
    var mu sync.Mutex
    semaphore := make(chan struct{}, maxConcurrent)

    for _, domain := range domains {
        wg.Add(1)
        go func(d Domain) {
            defer wg.Done()
            semaphore <- struct{}{}
            defer func() { <-semaphore }()

            if err := limiter.Wait(context.Background()); err != nil {
                fmt.Printf("限速器错误: %v\n", err)
                return
            }

            shouldCheck, failureCount := shouldCheckDomain(db, d)
            if !shouldCheck {
                fmt.Printf("跳过域名 %s://%s:%s (失败次数: %d)\n", d.Protocol, d.Host, d.Port, failureCount)
                return
            }

            if isValidDomain(d) {
                mu.Lock()
                validDomains = append(validDomains, d)
                mu.Unlock()
                fmt.Printf("有效域名: %s://%s:%s\n", d.Protocol, d.Host, d.Port)
                updateDomainStatus(db, d, true)
            } else {
                updateDomainStatus(db, d, false)
            }
        }(domain)
    }

    wg.Wait()

    fmt.Printf("检查完成，有效域名数量: %d\n", len(validDomains))
    return validDomains
}

func initDB() (*sql.DB, error) {
    db, err := sql.Open("sqlite3", *dbPath)
    if err != nil {
        return nil, err
    }

    _, err = db.Exec(`
        CREATE TABLE IF NOT EXISTS domain_checks (
            domain TEXT PRIMARY KEY,
            failure_count INT,
            last_check DATETIME
        )
    `)
    if err != nil {
        return nil, err
    }

    return db, nil
}

func shouldCheckDomain(db *sql.DB, domain Domain) (bool, int) {
    var failureCount int
    var lastCheck time.Time
    err := db.QueryRow("SELECT failure_count, last_check FROM domain_checks WHERE domain = ?", 
                       fmt.Sprintf("%s://%s:%s", domain.Protocol, domain.Host, domain.Port)).
           Scan(&failureCount, &lastCheck)

    if err == sql.ErrNoRows {
        return true, 0
    } else if err != nil {
        fmt.Printf("查询域名状态失败: %v\n", err)
        return true, 0
    }

    if failureCount >= *failureThreshold && time.Since(lastCheck) < time.Duration(*silentDays)*24*time.Hour {
        return false, failureCount
    }

    return true, failureCount
}

func updateDomainStatus(db *sql.DB, domain Domain, isValid bool) {
    domainStr := fmt.Sprintf("%s://%s:%s", domain.Protocol, domain.Host, domain.Port)
    if isValid {
        _, err := db.Exec("DELETE FROM domain_checks WHERE domain = ?", domainStr)
        if err != nil {
            fmt.Printf("删除有效域名记录失败: %v\n", err)
        }
    } else {
        _, err := db.Exec(`
            INSERT INTO domain_checks (domain, failure_count, last_check)
            VALUES (?, 1, CURRENT_TIMESTAMP)
            ON CONFLICT(domain) DO UPDATE SET
            failure_count = failure_count + 1,
            last_check = CURRENT_TIMESTAMP
        `, domainStr)
        if err != nil {
            fmt.Printf("更新域名状态失败: %v\n", err)
        }
    }
}

func isValidDomain(domain Domain) bool {
    url := fmt.Sprintf("%s://%s:%s", domain.Protocol, domain.Host, domain.Port)
    client := &http.Client{
        Timeout: timeout,
        Transport: &http.Transport{
            TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
        },
    }

    for i := 0; i < maxRetries; i++ {
        resp, err := client.Get(url)
        if err != nil {
            time.Sleep(time.Second * time.Duration(i+1))
            continue
        }
        defer resp.Body.Close()

        body, err := ioutil.ReadAll(resp.Body)
        if err != nil {
            fmt.Printf("读取 %s 的响应体时出错: %v\n", url, err)
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

        // 检查响应体是否包含特定字符串
        if strings.Contains(bodyString, "Sansui233") && strings.Contains(bodyString, "目前共有抓取源") {
            return true
        }
        break
    }
    return false
}
