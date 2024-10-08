package main

import (
    "flag"
    "fmt"
    "os"
)

func main() {
    // 定义自定义的 FlagSet
    fs := flag.NewFlagSet("", flag.ContinueOnError)
    fs.Usage = func() {} // 覆盖默认的 Usage 函数，使其不输出任何内容

    // 定义命令行参数
    var inputPath string
    var outputDir string

    fs.StringVar(&inputPath, "i", "domains.txt", "输入文件路径")
    fs.StringVar(&inputPath, "input", "domains.txt", "输入文件路径")
    fs.StringVar(&outputDir, "o", ".", "输出文件目录")
    fs.StringVar(&outputDir, "output", ".", "输出文件目录")

    // 解析命令行参数，忽略错误
    _ = fs.Parse(os.Args[1:])

    // 打印所有输入的参数
    fmt.Printf("输入文件路径: %s\n", inputPath)
    fmt.Printf("输出文件目录: %s\n", outputDir)

    // 这里可以添加原来 main 函数的其他逻辑
    fmt.Println("开始处理域名...")

    // ... (其他代码逻辑)

    fmt.Println("处理完成。")
}
