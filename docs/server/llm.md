# Local LLM Support

Run large language models locally on Apple Silicon using MLX.

## Requirements

- macOS 13+ (Ventura or later)
- Apple Silicon (M1/M2/M3/M4)
- Minimum 8GB RAM (16GB+ recommended for larger models)

## How It Works

Basepod uses [MLX](https://github.com/ml-explore/mlx) to run LLMs natively on Apple Silicon, leveraging the unified memory architecture for efficient inference.

- **No Docker container** - Runs natively for best performance
- **OpenAI-compatible API** - Drop-in replacement for OpenAI
- **Automatic model management** - Download, run, and switch models easily

## Available Models

### Chat Models

| Model | Size | RAM Required |
|-------|------|--------------|
| Llama-3.2-1B | 0.8GB | 1GB |
| Llama-3.2-3B | 2.1GB | 3GB |
| Mistral-7B | 4.5GB | 5GB |
| Gemma-2-9B | 5.5GB | 6GB |
| Phi-4 | 8.5GB | 9GB |
| Llama-3.1-70B | 40GB | 42GB |

### Code Models

| Model | Size | RAM Required |
|-------|------|--------------|
| Qwen2.5-Coder-1.5B | 1.2GB | 2GB |
| Qwen2.5-Coder-7B | 4.2GB | 5GB |
| DeepSeek-Coder-6.7B | 4.0GB | 5GB |
| Codestral-22B | 13GB | 14GB |

### Vision Models

| Model | Size | RAM Required |
|-------|------|--------------|
| Qwen2-VL-2B | 1.5GB | 2GB |
| LLaVA-1.6-7B | 4.5GB | 5GB |
| PaliGemma-3B | 2.0GB | 3GB |

### Embedding Models

| Model | Size | RAM Required |
|-------|------|--------------|
| BGE-Small | 0.1GB | 0.5GB |
| BGE-Large | 0.4GB | 1GB |
| GTE-Large | 0.4GB | 1GB |

## CLI Usage

### List Models

```bash
bp models
```

Output:
```
DOWNLOADED:
  NAME                           SIZE    RAM
  mlx-community/Llama-3.2-3B     2.1GB   3GB

AVAILABLE:
  mlx-community/Llama-3.2-1B     0.8GB   1GB
  mlx-community/Mistral-7B       4.5GB   5GB
  ...
```

### Download a Model

```bash
bp model pull Llama-3.2-3B
```

Progress:
```
Pulling mlx-community/Llama-3.2-3B-Instruct-4bit...
Downloading: 67% [========>    ] 1.4GB/2.1GB  15.2 MB/s  ETA: 45s
```

### Start LLM Server

```bash
bp model run Llama-3.2-3B
```

Output:
```
Starting LLM server with Llama-3.2-3B...
Server running at: https://llm.example.com
API endpoint: https://llm.example.com/v1/chat/completions
```

### Chat Interactively

```bash
bp chat
```

### Stop Server

```bash
bp model stop
```

### Delete Model

```bash
bp model rm Llama-3.2-3B
```

## API Usage

The LLM server provides an OpenAI-compatible API at `https://llm.example.com`.

### Chat Completion

```bash
curl https://llm.example.com/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "default",
    "messages": [
      {"role": "user", "content": "Hello, how are you?"}
    ],
    "max_tokens": 1000,
    "temperature": 0.7
  }'
```

### Using with OpenAI SDK

```python
from openai import OpenAI

client = OpenAI(
    base_url="https://llm.example.com/v1",
    api_key="not-needed"  # No auth required for local
)

response = client.chat.completions.create(
    model="default",
    messages=[
        {"role": "user", "content": "Write a haiku about coding"}
    ]
)

print(response.choices[0].message.content)
```

### Using with JavaScript

```javascript
const response = await fetch('https://llm.example.com/v1/chat/completions', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({
    model: 'default',
    messages: [{ role: 'user', content: 'Hello!' }]
  })
});

const data = await response.json();
console.log(data.choices[0].message.content);
```

## Configuration

### Default Settings

| Setting | Default | Description |
|---------|---------|-------------|
| `max_tokens` | 4096 | Maximum tokens to generate |
| `temperature` | 0.7 | Creativity (0-1) |
| `context_size` | 8192 | Context window size |

### Override in Request

```json
{
  "model": "default",
  "messages": [...],
  "max_tokens": 2000,
  "temperature": 0.3
}
```

## Performance Tips

1. **Use 4-bit quantized models** - Same quality, 4x less RAM
2. **Close other apps** - More RAM = faster inference
3. **Smaller models for simple tasks** - 1B-3B models are fast for basic Q&A
4. **Larger models for complex tasks** - 7B+ for coding, reasoning

## Troubleshooting

### "Out of memory"

- Use a smaller model
- Close memory-intensive apps
- Check RAM usage: `bp models --downloaded`

### "Model not found"

```bash
bp model pull <model-name>
```

### "Server not responding"

Check if the server is running:
```bash
bp model status
```

Restart if needed:
```bash
bp model stop
bp model run <model>
```

### "Slow generation"

- 4-bit models are faster than 8-bit
- Smaller models generate faster
- Reduce `max_tokens` if you don't need long outputs
