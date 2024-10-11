package main

import (
    "bufio"
    "io"
    "fmt"
    "context"
    "crypto/tls"
    "flag"
    "log"
    "net"
    "net/http"
    "net/url"
    "os"
    "strings"
    "sync"
    "sync/atomic"
    "time"

    c "github.com/metacubex/mihomo/config"
    "gopkg.in/yaml.v2"
)

func main() {
	// 定义命令行参数
	inputFile := flag.String("i", "domains.txt", "输入文件路径")
	flag.StringVar(inputFile, "input", "domains.txt", "输入文件路径")

	outputDir := flag.String("o", ".", "输出文件目录")
	flag.StringVar(outputDir, "output", ".", "输出文件目录")

	dbFile := flag.String("d", "proxy.db", "SQLite数据库文件路径")
	flag.StringVar(dbFile, "database", "proxy.db", "SQLite数据库文件路径")

	errorThreshold := flag.Int("e", 3, "错误次数阈值")
	flag.IntVar(errorThreshold, "error-threshold", 3, "错误次数阈值")

	refreshDays := flag.Int("r", 7, "刷新天数")
	flag.IntVar(refreshDays, "refresh-days", 7, "刷新天数")

	// 解析命令行参数
	flag.Parse()

	// 打印所有输入的参数
	log.Printf("输入文件: %s", *inputFile)
	log.Printf("输出目录: %s", *outputDir)
	log.Printf("数据库文件: %s", *dbFile)
	log.Printf("错误次数阈值: %d", *errorThreshold)
	log.Printf("刷新天数: %d", *refreshDays)

	// 读取域名
	domains := readDomains(*inputFile)
	
	// 检查域名
	validDomains := checkDomains(domains)

	    proxiesMap := fetchAndParseProxies(validDomains)

    // 输出代理数量
    proxyCount := 0
    proxiesMap.Range(func(key, value interface{}) bool {
        proxyCount++
        return true
    })
    log.Printf("成功解析的代理数量: %d", proxyCount)

	// 注意：outputDir, dbFile, errorThreshold, 和 refreshDays 
	// 在这个示例中没有被直接使用，但它们可能在后续的功能实现中用到

	log.Println("程序执行完毕")
}

// readDomains 函数从指定文件中读取域名，进行格式化和去重，并返回有效的域名列表
// 参数 inputFile 是输入文件的路径
// 返回值是一个 *sync.Map，其中包含所有有效且唯一的域名
func readDomains(inputFile string) *sync.Map {
	// 初始化 sync.Map 用于存储最终的域名列表，以及一个 map 用于去重
	// 预分配 uniqueDomains 的容量为 1000，以减少 map 扩容次数
	domains, uniqueDomains := &sync.Map{}, make(map[string]struct{}, 1000)

	// 打开输入文件
	file, err := os.Open(inputFile)
	if err != nil {
		log.Printf("无法打开输入文件: %v", err)
		return domains // 如果无法打开文件，返回空的 domains
	}
	defer file.Close() // 确保文件在函数结束时关闭

	lineCount, validDomainCount := 0, 0 // 初始化行数和有效域名计数器
	scanner := bufio.NewScanner(file)
	// 增加 scanner 的缓冲区大小到 1MB，提高读取效率
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	// 逐行读取文件内容
	for scanner.Scan() {
		lineCount++ // 增加行数计数
		// 解析每一行为 URL
		if u, err := url.Parse(strings.TrimSpace(scanner.Text())); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
			// 提取主机名、端口，并判断是否为默认端口
			host, port, isDefaultPort := u.Hostname(), u.Port(), (u.Scheme == "http" && u.Port() == "80") || (u.Scheme == "https" && u.Port() == "443")
			// 构造用于去重的 key
			dedupeKey := host + (map[bool]string{true: "", false: ":" + port}[isDefaultPort || port == ""])
			// 检查是否为重复域名
			if _, exists := uniqueDomains[dedupeKey]; !exists {
				// 如果不是重复域名，添加到 uniqueDomains 并增加有效域名计数
				uniqueDomains[dedupeKey], validDomainCount = struct{}{}, validDomainCount+1
				// 构造格式化的 URL
				formattedURL := u.Scheme + "://" + host + (map[bool]string{true: "", false: ":" + port}[isDefaultPort || port == ""])
				// 将格式化的 URL 存储到 domains 中
				domains.Store(formattedURL, struct{}{})
			}
		}
	}

	// 检查是否在读取过程中发生错误
	if err := scanner.Err(); err != nil {
		log.Printf("读取文件时出错: %v", err)
		return &sync.Map{} // 如果发生错误，返回空的 sync.Map
	}

	// 输出统计信息
	log.Printf("总行数: %d, 有效域名数: %d", lineCount, validDomainCount)
	return domains // 返回处理后的域名列表
}

func checkDomains(domains *sync.Map) *sync.Map {
    const (
        concurrency, tcpTimeout, tcpRetries, reportInterval = 50, 5 * time.Second, 3, 5 * time.Second
    )
    validDomains, total, success := &sync.Map{}, int64(0), int64(0)
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    go func() {
        for range time.Tick(reportInterval) {
            select {
            case <-ctx.Done(): return
            default: log.Printf("进度：总数 %d，成功 %d", atomic.LoadInt64(&total), atomic.LoadInt64(&success))
            }
        }
    }()

    var wg sync.WaitGroup
    sem, dialer := make(chan struct{}, concurrency), &net.Dialer{Timeout: tcpTimeout}

    domains.Range(func(key, _ interface{}) bool {
        wg.Add(1)
        go func(domain string) {
            defer wg.Done(); sem <- struct{}{}; defer func() { <-sem }()
            atomic.AddInt64(&total, 1)
            u, err := url.Parse(domain)
            if err != nil { return }
            host, port := u.Hostname(), u.Port()
            if port == "" { port = map[string]string{"https": "443", "http": "80"}[u.Scheme] }
            for i := 0; i < tcpRetries; i++ {
                if conn, err := dialer.Dial("tcp", net.JoinHostPort(host, port)); err == nil {
                    conn.Close(); validDomains.Store(domain, struct{}{}); atomic.AddInt64(&success, 1); return
                }
                time.Sleep(time.Second)
            }
        }(key.(string))
        return true
    })

    wg.Wait(); cancel()
    log.Printf("最终结果：总数 %d，成功 %d", atomic.LoadInt64(&total), atomic.LoadInt64(&success))
    return validDomains
}

func fetchAndParseProxies(validDomains *sync.Map) *sync.Map {
    proxiesMap, stats := &sync.Map{}, struct{ total, unique, sites, duplicates, available, checked int64 }{}
    var wg sync.WaitGroup
    sem := make(chan struct{}, 50)
    client := &http.Client{
        Timeout: 10 * time.Second,
        Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
    }

    validDomains.Range(func(key, _ interface{}) bool {
        wg.Add(1)
        go func(domain string) {
            defer wg.Done()
            sem <- struct{}{}; defer func() { <-sem }()
            req, err := http.NewRequest("GET", domain+"/clash/proxies", nil)
            if err != nil {
                log.Printf("创建请求失败 %s: %v", domain, err)
                return
            }
            req.Header.Set("User-Agent", "ClashForAndroid/2.5.12")
            resp, err := client.Do(req)
            if err != nil || resp == nil || resp.StatusCode != http.StatusOK {
                log.Printf("获取 %s 失败: %v", domain, err)
                return
            }
            defer resp.Body.Close()
	    bodyBytes, err := io.ReadAll(resp.Body)
	    if err != nil {
		return
	    }
             
	    rawConfig, err := c.UnmarshalRawConfig(bodyBytes)
	    if err != nil {
	        log.Printf("解析 %s 的YAML失败或无有效代理: %v", domain, err)
	        return
	    }
	    _ , err = c.ParseRawConfig(rawConfig)
	    log.Printf("解析 %s 测试结果: %v", domain, err)
            var config struct{ Proxies []map[string]interface{} `yaml:"proxies"` }
            if err := yaml.NewDecoder(resp.Body).Decode(&config); err != nil || len(config.Proxies) == 0 {
                log.Printf("解析 %s 的YAML失败或无有效代理: %v", domain, err)
                return
            }
            atomic.AddInt64(&stats.sites, 1)
            siteDuplicates := 0
            for _, proxy := range config.Proxies {
                atomic.AddInt64(&stats.total, 1)
                if proxyType, ok := proxy["type"].(string); ok {
                    key := fmt.Sprintf("%s|%v|%v|%v", proxyType, proxy["server"], proxy["port"], proxy["password"])
                    switch proxyType {
                    case "ss":
                        key += fmt.Sprintf("|%v", proxy["cipher"])
                    case "ssr":
                        key += fmt.Sprintf("|%v|%v|%v", proxy["cipher"], proxy["protocol"], proxy["obfs"])
                    case "vmess":
                        key += fmt.Sprintf("|%v|%v", proxy["uuid"], proxy["alterId"])
                    case "trojan":
                        key += fmt.Sprintf("|%v", proxy["sni"])
                    default:
                        continue
                    }
                    if _, loaded := proxiesMap.LoadOrStore(key, proxy); !loaded {
                        atomic.AddInt64(&stats.unique, 1)
                    } else {
                        siteDuplicates++
                        atomic.AddInt64(&stats.duplicates, 1)
                    }
                }
            }
            log.Printf("从 %s 解析到 %d 个代理，其中重复 %d 个", domain, len(config.Proxies), siteDuplicates)
        }(key.(string))
        return true
    })

    wg.Wait()

    // 启动进度显示 goroutine
    done := make(chan bool)
    go func() {
        ticker := time.NewTicker(1 * time.Second)
        defer ticker.Stop()
        for {
            select {
            case <-done:
                return
            case <-ticker.C:
                log.Printf("进度: 总数 %d, 已检测 %d, 可用 %d", 
                           stats.unique, stats.checked, stats.available)
            }
        }
    }()

    // TCPing 测试
    tcpingSem := make(chan struct{}, 200) // 限制并发 TCPing 的数量
    proxiesMap.Range(func(key, value interface{}) bool {
        wg.Add(1)
        go func(k string, v map[string]interface{}) {
            defer wg.Done()
            tcpingSem <- struct{}{}
            defer func() { <-tcpingSem }()

            server, _ := v["server"].(string)
            addr := fmt.Sprintf("%s:%v", server, v["port"])
            for i := 0; i < 3; i++ { // 重试 3 次
                conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
                if err == nil {
                    conn.Close()
                    atomic.AddInt64(&stats.available, 1)
                    atomic.AddInt64(&stats.checked, 1)
                    return
                }
                time.Sleep(1 * time.Second)
            }
            atomic.AddInt64(&stats.checked, 1)
            proxiesMap.Delete(k) // 如果 3 次都失败，删除这个代理
        }(key.(string), value.(map[string]interface{}))
        return true
    })

    wg.Wait()
    done <- true // 停止进度显示

    log.Printf("最终结果: 成功站点: %d, 总代理: %d, 唯一代理: %d, 总重复代理: %d, 可用代理: %d", 
               stats.sites, stats.total, stats.unique, stats.duplicates, stats.available)
    return proxiesMap
}
