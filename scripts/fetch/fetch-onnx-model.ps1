param(
    [string]$DestDir = (Join-Path (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path ".mcp-semantic-search-zvec-go\models\paraphrase-multilingual-MiniLM-L12-v2")
)

$ErrorActionPreference = "Stop"
$ModelURL = if ($env:ONNX_MODEL_URL) { $env:ONNX_MODEL_URL } else {
    "https://huggingface.co/Xenova/paraphrase-multilingual-MiniLM-L12-v2/resolve/main/onnx/model.onnx"
}
$TokenizerURL = if ($env:ONNX_TOKENIZER_URL) { $env:ONNX_TOKENIZER_URL } else {
    "https://huggingface.co/Xenova/paraphrase-multilingual-MiniLM-L12-v2/resolve/main/tokenizer.json"
}

function Verify-Sha256 {
    param([string]$Path, [string]$Want)
    if (-not $Want) { return }
    $got = (Get-FileHash -Algorithm SHA256 -Path $Path).Hash.ToLower()
    if ($got -ne $Want.ToLower()) {
        throw "checksum mismatch for $Path`: got $got want $Want"
    }
}

New-Item -ItemType Directory -Force -Path $DestDir | Out-Null
$modelPath = Join-Path $DestDir "model_optimized.onnx"
$tokenizerPath = Join-Path $DestDir "tokenizer.json"

Write-Host "Downloading model_optimized.onnx..."
Invoke-WebRequest -Uri $ModelURL -OutFile $modelPath -UseBasicParsing
Write-Host "Downloading tokenizer.json..."
Invoke-WebRequest -Uri $TokenizerURL -OutFile $tokenizerPath -UseBasicParsing

Verify-Sha256 $modelPath $env:ONNX_MODEL_SHA256
Verify-Sha256 $tokenizerPath $env:ONNX_TOKENIZER_SHA256

Write-Host "ONNX model bundle ready at $DestDir"
