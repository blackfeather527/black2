package main

import (
    "flag"
    "fmt"
    "os"
)

func main() {
    // 定义命令行参数
    var (
        inputFile  string
        outputDir  string
        showHelp   bool
    )

    // 设置命令行参数
    flag.StringVar(&inputFile, "i", "domains.txt", "输入文件路径")
    flag.StringVar(&inputFile, "input", "domains.txt", "输入文件路径")
    flag.StringVar(&outputDir, "o", ".", "输出文件目录")
    flag.StringVar(&outputDir, "output", ".", "输出文件目录")
    flag.BoolVar(&showHelp, "h", false, "显示帮助信息")
    flag.BoolVar(&showHelp, "help", false, "显示帮助信息")

    // 自定义Usage函数来忽略未定义的参数
    flag.Usage = func() {
        fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
        flag.PrintDefaults()
    }

    // 解析命令行参数
    flag.Parse()

    // 显示帮助信息
    if showHelp {
        flag.Usage()
        return
    }

    // 打印所有输入的参数
    fmt.Printf("输入文件: %s\n", inputFile)
    fmt.Printf("输出目录: %s\n", outputDir)

    // 打印未解析的参数
    if len(flag.Args()) > 0 {
        fmt.Println("未解析的参数:", flag.Args())
    }

    // 主程序逻辑
    fmt.Println("开始处理域名...")

    // 这里添加您的主要处理逻辑
    // ...

    fmt.Println("处理完成。")
}
