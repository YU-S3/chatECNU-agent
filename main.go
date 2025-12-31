package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/sashabaranov/go-openai"
)

// Tool 定义可用的工具
type Tool struct {
	Type        string                 `json:"type"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

// ToolCall 表示工具调用请求
type ToolCall struct {
	ID       string                 `json:"id"`
	Type     string                 `json:"type"`
	Function ToolFunction           `json:"function"`
}

// ToolFunction 表示工具函数
type ToolFunction struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

// ToolResult 表示工具执行结果
type ToolResult struct {
	ToolCallID string `json:"tool_call_id"`
	Role       string `json:"role"`
	Content    string `json:"content"`
}

// ECNUAgent ChatECNU Agent实现
type ECNUAgent struct {
	client      *openai.Client
	model       string
	tools       []Tool
	history     []openai.ChatCompletionMessage
	maxHistory  int
	workingDir  string
}

// NewECNUAgent 创建新的Agent实例
func NewECNUAgent(apiKey string) (*ECNUAgent, error) {
	// 加载环境变量
	godotenv.Load()

	// 从环境变量获取API密钥（如果未提供）
	if apiKey == "" {
		apiKey = os.Getenv("ECNU_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("ECNU_API_KEY环境变量未设置")
		}
	}

	// 获取工作目录
	wd, err := os.Getwd()
	if err != nil {
		wd = "."
	}

	// 创建OpenAI兼容客户端（chatECNU使用OpenAI兼容API）
	config := openai.DefaultConfig(apiKey)
	config.BaseURL = "https://chat.ecnu.edu.cn/open/api/v1"
	client := openai.NewClientWithConfig(config)

	agent := &ECNUAgent{
		client:     client,
		model:      "ecnu-plus", // 使用推荐的模型
		maxHistory: 20,          // 限制历史记录数量
		workingDir: wd,
	}

	// 初始化工具列表
	agent.initTools()

	// 初始化系统提示
	agent.initSystemPrompt()

	return agent, nil
}

// initTools 初始化可用工具
func (a *ECNUAgent) initTools() {
	a.tools = []Tool{
		{
			Type:        "function",
			Name:        "execute_command",
			Description: "在Linux命令行环境中执行系统命令。可以执行任何shell命令，包括管道、重定向等复杂操作。返回命令的标准输出、标准错误和退出码。",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"command": map[string]interface{}{
						"type":        "string",
						"description": "要执行的完整命令，可以包含管道、重定向等",
					},
					"timeout": map[string]interface{}{
						"type":        "integer",
						"description": "命令超时时间（秒），默认30秒",
						"default":     30,
					},
				},
				"required": []string{"command"},
			},
		},
		{
			Type:        "function",
			Name:        "read_file",
			Description: "读取文件内容。支持文本文件，自动处理UTF-8编码。如果文件不存在或无法读取，返回错误信息。",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "要读取的文件路径（绝对路径或相对路径）",
					},
				},
				"required": []string{"path"},
			},
		},
		{
			Type:        "function",
			Name:        "write_file",
			Description: "写入或创建文件。如果文件不存在会自动创建，如果目录不存在会自动创建父目录。写入前会先读取文件内容（如果存在）进行确认。",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "要写入的文件路径（绝对路径或相对路径）",
					},
					"content": map[string]interface{}{
						"type":        "string",
						"description": "要写入的文件内容",
					},
					"append": map[string]interface{}{
						"type":        "boolean",
						"description": "是否追加模式，默认false（覆盖）",
						"default":     false,
					},
				},
				"required": []string{"path", "content"},
			},
		},
		{
			Type:        "function",
			Name:        "list_directory",
			Description: "列出目录内容。返回目录中的文件和子目录列表。",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "要列出的目录路径（绝对路径或相对路径），默认为当前工作目录",
						"default":     ".",
					},
				},
			},
		},
		{
			Type:        "function",
			Name:        "get_working_directory",
			Description: "获取当前工作目录的绝对路径。",
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
	}
}

// initSystemPrompt 初始化系统提示
func (a *ECNUAgent) initSystemPrompt() {
	currentUser, _ := user.Current()
	username := "unknown"
	if currentUser != nil {
		username = currentUser.Username
	}

	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "unknown"
	}

	systemPrompt := fmt.Sprintf(`你是一个强大的AI助手，被设计为一个可以在Linux命令行环境中执行任务的智能代理。

环境信息：
- 当前工作目录: %s
- 当前用户: %s
- 主机名: %s
- 当前时间: %s

重要规则：
1. 你可以使用提供的工具来执行命令、读写文件、列出目录等操作。
2. 在执行任何写入文件或修改系统的关键操作前，务必先读取文件内容或检查当前状态，确认后再执行。
3. 你拥有执行系统命令的权限，如果需要sudo权限，可以在命令前加'sudo'。
4. 每次只执行一个工具调用，等待结果后再决定下一步操作。
5. 你的回答应该简洁明了，专注于任务本身。
6. 如果遇到错误，分析错误信息并尝试修复。
7. 完成任务后，使用自然语言向用户说明结果。

请使用工具来完成用户的任务。`, a.workingDir, username, hostname, time.Now().Format("2006-01-02 15:04:05"))

	a.history = []openai.ChatCompletionMessage{
		{
			Role:    openai.ChatMessageRoleSystem,
			Content: systemPrompt,
		},
	}
}

// truncateHistory 截断历史记录以控制上下文长度
func (a *ECNUAgent) truncateHistory() {
	if len(a.history) <= a.maxHistory {
		return
	}

	// 保留系统消息和最近的对话
	newHistory := []openai.ChatCompletionMessage{a.history[0]} // 系统消息
	startIdx := len(a.history) - a.maxHistory + 1
	if startIdx < 1 {
		startIdx = 1
	}
	newHistory = append(newHistory, a.history[startIdx:]...)
	a.history = newHistory
}

// callModel 调用chatECNU API
func (a *ECNUAgent) callModel(ctx context.Context, userInput string, maxRetries int) (*openai.ChatCompletionResponse, error) {
	// 添加用户消息
	if userInput != "" {
		a.history = append(a.history, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleUser,
			Content: userInput,
		})
	}

	// 截断历史
	a.truncateHistory()

	// 准备工具定义
	tools := make([]openai.Tool, len(a.tools))
	for i, tool := range a.tools {
		paramsBytes, _ := json.Marshal(tool.Parameters)
		tools[i] = openai.Tool{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  paramsBytes,
			},
		}
	}

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(attempt) * time.Second
			log.Printf("[重试 %d/%d] 等待 %v 后重试...\n", attempt+1, maxRetries, backoff)
			time.Sleep(backoff)
		}

		req := openai.ChatCompletionRequest{
			Model:       a.model,
			Messages:    a.history,
			Temperature: 0.2,
			Tools:       tools,
		}

		resp, err := a.client.CreateChatCompletion(ctx, req)
		if err != nil {
			lastErr = err
			log.Printf("[错误] API调用失败 (尝试 %d/%d): %v\n", attempt+1, maxRetries, err)
			continue
		}

		return &resp, nil
	}

	return nil, fmt.Errorf("API调用失败，已重试%d次: %v", maxRetries, lastErr)
}

// executeTool 执行工具调用
func (a *ECNUAgent) executeTool(toolCall openai.ToolCall) (string, error) {
	function := toolCall.Function
	name := function.Name
	args := function.Arguments

	log.Printf("[工具调用] %s\n", name)
	log.Printf("[参数] %s\n", args)

	switch name {
	case "execute_command":
		return a.executeCommand(args)
	case "read_file":
		return a.readFile(args)
	case "write_file":
		return a.writeFile(args)
	case "list_directory":
		return a.listDirectory(args)
	case "get_working_directory":
		return a.getWorkingDirectory(args)
	default:
		return "", fmt.Errorf("未知的工具: %s", name)
	}
}

// executeCommand 执行系统命令
func (a *ECNUAgent) executeCommand(args string) (string, error) {
	var params map[string]interface{}
	if err := json.Unmarshal([]byte(args), &params); err != nil {
		return "", fmt.Errorf("解析参数失败: %v", err)
	}

	command, ok := params["command"].(string)
	if !ok {
		return "", fmt.Errorf("缺少command参数")
	}

	timeout := 30
	if t, ok := params["timeout"].(float64); ok {
		timeout = int(t)
	}

	log.Printf("[执行命令] %s (超时: %d秒)\n", command, timeout)

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = a.workingDir
	output, err := cmd.CombinedOutput()

	var exitCode int
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	} else if err != nil {
		exitCode = -1
	}

	result := fmt.Sprintf("命令: %s\n退出码: %d\n", command, exitCode)
	if len(output) > 0 {
		result += fmt.Sprintf("输出:\n%s", string(output))
	}
	if err != nil && ctx.Err() == context.DeadlineExceeded {
		result += fmt.Sprintf("\n错误: 命令执行超时（%d秒）", timeout)
	} else if err != nil {
		result += fmt.Sprintf("\n错误: %v", err)
	}

	return result, nil
}

// readFile 读取文件
func (a *ECNUAgent) readFile(args string) (string, error) {
	var params map[string]interface{}
	if err := json.Unmarshal([]byte(args), &params); err != nil {
		return "", fmt.Errorf("解析参数失败: %v", err)
	}

	path, ok := params["path"].(string)
	if !ok {
		return "", fmt.Errorf("缺少path参数")
	}

	// 解析路径
	fullPath := path
	if !filepath.IsAbs(path) {
		fullPath = filepath.Join(a.workingDir, path)
	}
	fullPath = filepath.Clean(fullPath)

	log.Printf("[读取文件] %s\n", fullPath)

	content, err := os.ReadFile(fullPath)
	if err != nil {
		return fmt.Sprintf("读取文件失败: %v", err), nil
	}

	return fmt.Sprintf("文件内容 (%s):\n%s", fullPath, string(content)), nil
}

// writeFile 写入文件
func (a *ECNUAgent) writeFile(args string) (string, error) {
	var params map[string]interface{}
	if err := json.Unmarshal([]byte(args), &params); err != nil {
		return "", fmt.Errorf("解析参数失败: %v", err)
	}

	path, ok := params["path"].(string)
	if !ok {
		return "", fmt.Errorf("缺少path参数")
	}

	content, ok := params["content"].(string)
	if !ok {
		return "", fmt.Errorf("缺少content参数")
	}

	append := false
	if a, ok := params["append"].(bool); ok {
		append = a
	}

	// 解析路径
	fullPath := path
	if !filepath.IsAbs(path) {
		fullPath = filepath.Join(a.workingDir, path)
	}
	fullPath = filepath.Clean(fullPath)

	log.Printf("[写入文件] %s (追加: %v)\n", fullPath, append)

	// 创建父目录
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return "", fmt.Errorf("创建目录失败: %v", err)
	}

	// 写入文件
	flags := os.O_WRONLY | os.O_CREATE
	if append {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}

	file, err := os.OpenFile(fullPath, flags, 0644)
	if err != nil {
		return "", fmt.Errorf("打开文件失败: %v", err)
	}
	defer file.Close()

	if _, err := file.WriteString(content); err != nil {
		return "", fmt.Errorf("写入文件失败: %v", err)
	}

	return fmt.Sprintf("成功写入文件: %s", fullPath), nil
}

// listDirectory 列出目录内容
func (a *ECNUAgent) listDirectory(args string) (string, error) {
	var params map[string]interface{}
	if err := json.Unmarshal([]byte(args), &params); err != nil {
		return "", fmt.Errorf("解析参数失败: %v", err)
	}

	path := "."
	if p, ok := params["path"].(string); ok && p != "" {
		path = p
	}

	// 解析路径
	fullPath := path
	if !filepath.IsAbs(path) {
		fullPath = filepath.Join(a.workingDir, path)
	}
	fullPath = filepath.Clean(fullPath)

	log.Printf("[列出目录] %s\n", fullPath)

	entries, err := os.ReadDir(fullPath)
	if err != nil {
		return fmt.Sprintf("读取目录失败: %v", err), nil
	}

	var result strings.Builder
	result.WriteString(fmt.Sprintf("目录内容 (%s):\n", fullPath))
	for _, entry := range entries {
		info, _ := entry.Info()
		typ := "文件"
		if entry.IsDir() {
			typ = "目录"
		}
		result.WriteString(fmt.Sprintf("  [%s] %s (大小: %d 字节)\n", typ, entry.Name(), info.Size()))
	}

	return result.String(), nil
}

// getWorkingDirectory 获取工作目录
func (a *ECNUAgent) getWorkingDirectory(args string) (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("获取工作目录失败: %v", err)
	}
	return fmt.Sprintf("当前工作目录: %s", wd), nil
}

// ProcessUserInput 处理用户输入
func (a *ECNUAgent) ProcessUserInput(ctx context.Context, userInput string) error {
	maxSteps := 20 // 防止无限循环
	stepCount := 0
	firstStep := true

	for stepCount < maxSteps {
		stepCount++
		log.Printf("\n[步骤 %d]\n", stepCount)

		// 只在第一步传入用户输入
		inputForModel := ""
		if firstStep {
			inputForModel = userInput
			firstStep = false
		}

		// 调用模型
		resp, err := a.callModel(ctx, inputForModel, 3)
		if err != nil {
			return fmt.Errorf("调用模型失败: %v", err)
		}

		if len(resp.Choices) == 0 {
			return fmt.Errorf("模型返回空响应")
		}

		choice := resp.Choices[0]
		message := choice.Message

		// 检查是否有工具调用
		if len(message.ToolCalls) > 0 {
			// 执行所有工具调用
			var toolResults []openai.ChatCompletionMessage
			for _, toolCall := range message.ToolCalls {
				result, err := a.executeTool(toolCall)
				if err != nil {
					result = fmt.Sprintf("工具执行失败: %v", err)
				}

				toolResults = append(toolResults, openai.ChatCompletionMessage{
					Role:       openai.ChatMessageRoleTool,
					Content:    result,
					ToolCallID: toolCall.ID,
				})
			}

			// 添加助手消息和工具结果到历史
			a.history = append(a.history, message)
			a.history = append(a.history, toolResults...)

			// 继续下一轮（不添加用户输入）
			continue
		}

		// 没有工具调用，显示最终回复
		if message.Content != "" {
			fmt.Printf("\n[助手] %s\n", message.Content)
			a.history = append(a.history, message)
			break
		}
	}

	if stepCount >= maxSteps {
		return fmt.Errorf("达到最大步骤数限制（%d步）", maxSteps)
	}

	return nil
}

// Run 运行交互式循环
func (a *ECNUAgent) Run() {
	fmt.Println("\n=== ChatECNU Agent 已启动 ===")
	fmt.Println("输入命令或'exit'退出\n")

	scanner := bufio.NewScanner(os.Stdin)
	ctx := context.Background()

	for {
		fmt.Print("用户> ")
		if !scanner.Scan() {
			break
		}

		userInput := strings.TrimSpace(scanner.Text())
		if userInput == "" {
			continue
		}

		if userInput == "exit" || userInput == "quit" {
			fmt.Println("再见！")
			break
		}

		if err := a.ProcessUserInput(ctx, userInput); err != nil {
			log.Printf("[错误] %v\n", err)
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("[错误] 读取输入失败: %v\n", err)
	}
}

func main() {
	agent, err := NewECNUAgent("")
	if err != nil {
		log.Fatalf("初始化Agent失败: %v\n", err)
	}

	agent.Run()
}

