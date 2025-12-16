# agent.py - 修正版 3: 增加重试机制以应对 API 504 等临时错误

import json
import os
import subprocess
import sys
import requests
import time # 引入 time 模块用于重试时的等待
from pathlib import Path


class ECNUChatAgent:
    def __init__(self, api_key):
        self.api_key = api_key
        self.base_url = "https://chat.ecnu.edu.cn/open/api/v1"
        self.session_history = [] # 维护对话历史
        self.max_context_length = 30000 # 设置一个安全的上下文长度限制，单位为token数的近似估计字符数
        # 修改点: 将 JSON 格式中的 { 和 } 进行转义 ({{ 和 }})
        self.system_prompt = """你是一个强大的AI助手，被设计为一个可以在Linux命令行环境中执行任务的智能代理。你的目标是理解和执行用户的指令，通过执行系统命令、读写文件等方式来完成任务。

重要规则：
1.  你必须严格遵循以下JSON格式来输出你的计划和行动，不允许有任何其他文字或解释。如果需要向用户输出信息，则使用 "action": "speak"。
    {{"action": "command", "command": "要执行的具体命令", "explanation": "为什么要执行此命令"}}
    {{"action": "read_file", "path": "/path/to/file", "explanation": "为什么要读取此文件"}}
    {{"action": "write_file", "path": "/path/to/file", "content": "文件的新内容", "explanation": "为什么要写入此文件"}}
    {{"action": "speak", "message": "你想对用户说的话", "explanation": "为什么需要说这句话"}}
2.  你拥有sudo权限，如果需要，可以直接在命令前加 'sudo'。
3.  在执行任何写入文件或修改系统的关键操作前，务必先读取文件内容，确认后再写入。
4.  每次只输出一个JSON对象。执行完该动作并收到结果后，再进行下一步。
5.  你的回答应该简洁明了，专注于任务本身。
6.  你当前的工作目录是: {working_dir}
7.  今天是: {current_date}"""

    def _truncate_history(self):
        """简单地按字符数截断历史记录以控制上下文长度"""
        total_chars = len(self.system_prompt)
        truncated_history = []
        # 逆序遍历，保留最新的消息
        for item in reversed(self.session_history):
            item_chars = len(json.dumps(item))
            if total_chars + item_chars > self.max_context_length:
                break
            truncated_history.insert(0, item)
            total_chars += item_chars
        self.session_history = truncated_history

    def _call_model(self, prompt, max_retries=3, backoff_factor=1): # 添加重试参数
        """调用ChatECNU API，包含重试逻辑"""
        headers = {
            "Authorization": f"Bearer {self.api_key}",
            "Content-Type": "application/json",
        }

        payload = {
            "model": "ecnu-plus", # 修改点: 使用官方推荐的 ecnu-plus 模型
            "messages": [
                {"role": "system", "content": self.system_prompt.format(working_dir=os.getcwd(), current_date="2025-12-10")}, # 注入当前工作目录和日期
            ] + self.session_history + [{"role": "user", "content": prompt}],
            "temperature": 0.2, # 较低的温度使输出更确定、一致
        }

        for attempt in range(max_retries):
            try:
                print(f"[DEBUG] Sending request to API (Attempt {attempt + 1}/{max_retries})...") # 调试信息，显示尝试次数
                response = requests.post(f"{self.base_url}/chat/completions", headers=headers, json=payload)
                
                # 检查是否是 504 错误或类似的上游错误
                if response.status_code == 504 or (response.status_code >= 500 and response.status_code < 600):
                    print(f"[WARNING] API returned status {response.status_code}. This might be a temporary issue. Attempt {attempt + 1}/{max_retries}.")
                    if attempt < max_retries - 1: # 如果不是最后一次尝试
                        sleep_time = backoff_factor * (2 ** attempt) # 指数退避
                        print(f"[INFO] Waiting {sleep_time} seconds before retrying...")
                        time.sleep(sleep_time)
                        continue # 继续下一次尝试
                    else:
                        # 所有重试都失败了
                        print(f"[ERROR] All {max_retries} attempts failed with status {response.status_code}.")
                        response.raise_for_status() # 抛出异常
                
                response.raise_for_status() # 对于非 5xx 错误，直接抛出异常
                response_data = response.json()

                print(f"[DEBUG] API Response: {response_data}") # 打印原始响应，用于调试

                if 'choices' in response_data and len(response_data['choices']) > 0:
                    content = response_data['choices'][0]['message']['content']
                    print(f"[DEBUG] Model Content: {content}") # 打印模型返回的内容

                    # 尝试解析模型返回的JSON
                    try:
                        action_json = json.loads(content.strip())
                        print(f"[DEBUG] Parsed Action: {action_json}") # 打印解析后的动作
                        return action_json
                    except json.JSONDecodeError as e:
                        print(f"[ERROR] Model response is not valid JSON:\n{content}\nError: {e}")
                        # 如果不是JSON，强制模型返回一个 speak 动作
                        return {"action": "speak", "message": f"模型返回了非预期格式: {content}", "explanation": "解析模型输出失败"}
                else:
                    print(f"[ERROR] Unexpected API response format: {response_data}")
                    return {"action": "speak", "message": "API返回了意外的格式。", "explanation": "API响应错误"}

            except requests.exceptions.RequestException as e:
                # 检查是否是 504 或其他上游错误导致的 RequestException
                if hasattr(e, 'response') and e.response is not None:
                    if e.response.status_code == 504 or (e.response.status_code >= 500 and e.response.status_code < 600):
                         print(f"[WARNING] RequestException with upstream error (status {e.response.status_code}). Attempt {attempt + 1}/{max_retries}.")
                         if attempt < max_retries - 1:
                            sleep_time = backoff_factor * (2 ** attempt)
                            print(f"[INFO] Waiting {sleep_time} seconds before retrying...")
                            time.sleep(sleep_time)
                            continue
                         else:
                            print(f"[ERROR] All {max_retries} attempts failed due to upstream error (status {e.response.status_code}).")
                
                error_msg = f"API call failed: {str(e)}"
                print(f"[ERROR] {error_msg}")
                return {"action": "speak", "message": error_msg, "explanation": "API调用失败"}
            except Exception as e:
                error_msg = f"An unexpected error occurred during API call: {str(e)}"
                print(f"[ERROR] {error_msg}")
                return {"action": "speak", "message": error_msg, "explanation": "发生未知错误"}

        # 如果循环结束仍未成功（理论上不应该到达这里，因为上面会返回），则返回错误
        error_msg = f"Failed to get a valid response from the API after {max_retries} attempts."
        print(f"[ERROR] {error_msg}")
        return {"action": "speak", "message": error_msg, "explanation": "API多次调用失败"}


    def _execute_action(self, action_obj):
        """执行模型返回的动作"""
        # 首先检查 action_obj 是否为字典
        if not isinstance(action_obj, dict):
            return {"result": f"Action object is not a dictionary: {action_obj}"}

        action_type = action_obj.get("action")
        explanation = action_obj.get("explanation", "")

        print(f"\n[INFO] Action: {action_type}. Reason: {explanation}")

        if action_type == "command":
            command = action_obj.get("command")
            if not command:
                return {"result": "No command provided in action object."}

            print(f"[EXEC] Running command: {command}")
            try:
                # 使用shell=True以支持管道、重定向等复杂命令
                result = subprocess.run(
                    command,
                    shell=True,
                    capture_output=True,
                    text=True,
                    timeout=30 # 设置超时时间，防止长时间挂起
                )
                stdout = result.stdout
                stderr = result.stderr
                return_code = result.returncode

                status = "SUCCESS" if return_code == 0 else "FAILED"
                output_summary = f"Command '{command}' finished with return code {return_code}.\nStatus: {status}\nSTDOUT:\n{stdout}\nSTDERR:\n{stderr}"
                print(output_summary)
                return {"result": output_summary}

            except subprocess.TimeoutExpired:
                timeout_error = f"Command '{command}' timed out after 30 seconds."
                print(timeout_error)
                return {"result": timeout_error}
            except Exception as e:
                exec_error = f"An error occurred while executing command '{command}': {str(e)}"
                print(exec_error)
                return {"result": exec_error}


        elif action_type == "read_file":
            file_path_str = action_obj.get("path")
            if not file_path_str:
                return {"result": "No file path provided in action object."}

            file_path = Path(file_path_str).resolve() # 解析为绝对路径
            print(f"[READ] Reading file: {file_path}")

            try:
                # 检查文件是否存在且为常规文件
                if not file_path.is_file():
                     return {"result": f"File does not exist or is not a regular file: {file_path}"}

                # 尝试以UTF-8读取
                try:
                    content = file_path.read_text(encoding='utf-8')
                except UnicodeDecodeError:
                    # 如果UTF-8失败，尝试latin-1作为备选
                    try:
                        content = file_path.read_text(encoding='latin-1')
                        print(f"[WARN] File {file_path} was read with 'latin-1' encoding due to UTF-8 decode error.")
                    except Exception:
                         return {"result": f"Could not read file {file_path} with common encodings."}

                success_msg = f"Successfully read file '{file_path}'. Content:\n{content}"
                print(success_msg)
                return {"result": success_msg}

            except PermissionError:
                 return {"result": f"Permission denied when reading file: {file_path}"}
            except Exception as e:
                 return {"result": f"An error occurred while reading file '{file_path}': {str(e)}"}


        elif action_type == "write_file":
            file_path_str = action_obj.get("path")
            content = action_obj.get("content")
            if not file_path_str or content is None: # content can be an empty string
                return {"result": "No file path or content provided in action object."}

            file_path = Path(file_path_str).resolve()
            print(f"[WRITE] Writing to file: {file_path}")
            print(f"[WRITE] Content:\n{content}")

            try:
                # 创建必要的父目录
                file_path.parent.mkdir(parents=True, exist_ok=True)

                # 写入文件
                with open(file_path, 'w', encoding='utf-8') as f:
                    f.write(content)
                success_msg = f"Successfully wrote content to file '{file_path}'."
                print(success_msg)
                return {"result": success_msg}

            except PermissionError:
                 return {"result": f"Permission denied when writing file: {file_path}"}
            except Exception as e:
                 return {"result": f"An error occurred while writing file '{file_path}': {str(e)}"}


        elif action_type == "speak":
             message = action_obj.get("message", "")
             print(f"\n[AGENT] {message}")
             return {"result": f"Agent spoke: {message}"}


        else:
             unknown_action = f"Unknown action type received: {action_type}"
             print(f"[ERROR] {unknown_action}")
             return {"result": unknown_action}


    def run(self):
        """主运行循环"""
        print("\n--- Linux Command Line AI Agent Started ---")
        print("Type your commands or 'exit' to quit.\n")

        while True:
            try:
                user_input = input("User> ").strip()
                if not user_input:
                    continue
                if user_input.lower() in ['exit', 'quit']:
                    print("Goodbye!")
                    break

                # 将用户输入添加到历史
                self.session_history.append({"role": "user", "content": user_input})

                # 循环执行模型的行动计划，直到它决定Speak
                max_steps_per_input = 10 # 防止无限循环的保险措施
                step_count = 0
                final_response = ""

                while step_count < max_steps_per_input:
                    step_count += 1
                    print(f"\n[STEP {step_count}]")

                    # 调用模型获取下一步行动
                    action_to_take = self._call_model(user_input if step_count == 1 else "") # 第一步传用户输入，后续不重复传

                    # 关键修复点: 确保 action_to_take 是一个有效的字典
                    if not isinstance(action_to_take, dict):
                        print(f"[ERROR] Received invalid action object: {action_to_take}")
                        break

                    # 执行行动并获取结果
                    execution_result = self._execute_action(action_to_take)

                    # 将行动和结果添加到历史，供下次调用模型时参考
                    self.session_history.append({"role": "assistant", "content": json.dumps(action_to_take)})
                    self.session_history.append({"role": "user", "content": f"Action result:\n{execution_result['result']}"})

                    # 检查是否是Speak动作，如果是，则结束本轮交互
                    if action_to_take.get("action") == "speak":
                        final_response = action_to_take.get("message", "")
                        # 不将 speak 的结果加入历史，因为它代表一轮对话的结束
                        # self.session_history.pop() # 如果需要，可以移除最后的result消息
                        break

                    # 简单限制历史长度
                    self._truncate_history()

                if final_response:
                    print(f"\n[FINAL AGENT RESPONSE] {final_response}")

            except KeyboardInterrupt:
                print("\n\nInterrupted by user. Goodbye!")
                break
            except Exception as e:
                # 关键修复点: 捕获所有异常，并打印详细信息
                print(f"\n[CRITICAL ERROR] An unexpected error occurred in the main loop: {e}")
                print(f"[CRITICAL ERROR] Error Type: {type(e).__name__}")
                import traceback
                traceback.print_exc() # 打印完整的堆栈跟踪
                break # 发生严重错误时退出


if __name__ == "__main__":
    # --- 配置 ---
    # 你需要在这里填入你的API密钥
    API_KEY = os.getenv("ECNU_API_KEY") # 推荐从环境变量加载
    if not API_KEY:
        print("Error: ECNU_API_KEY environment variable not set.")
        print("Please set it using: export ECNU_API_KEY='your_actual_api_key_here'")
        sys.exit(1)

    agent = ECNUChatAgent(api_key=API_KEY)
    agent.run()