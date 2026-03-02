#!/bin/bash

# 目标文件夹路径（根据需要修改或作为参数传入）
TARGET_DIR="/home/fish/go/src/cloud-media/proto/"

# 使用 ( ) 开启子 Shell 执行任务
(
  echo "正在进入目录: $TARGET_DIR"
  cd "$TARGET_DIR" || {
    echo "错误：无法进入目录"
    exit 1
  }

  echo "正在删除旧代码..."
  rm -rf "gen"

  echo "正在执行 buf generate..."
  buf generate

  echo "任务完成！"
)

# 子 Shell 结束后，主进程依然停留在执行脚本前的原始路径
