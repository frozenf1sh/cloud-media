#!/bin/bash

# 目标文件夹路径（根据需要修改或作为参数传入）
TARGET_DIR="/home/fish/go/src/cloud-media/cmd/api-server/"

# 使用 ( ) 开启子 Shell 执行任务
(
  echo "正在进入目录: $TARGET_DIR"
  cd "$TARGET_DIR" || {
    echo "错误：无法进入目录"
    exit 1
  }

  echo "正在执行 wire"
  wire

  echo "任务完成！"
)

# 目标文件夹路径（根据需要修改或作为参数传入）
TARGET_DIR="/home/fish/go/src/cloud-media/cmd/worker/"

# 使用 ( ) 开启子 Shell 执行任务
(
  echo "正在进入目录: $TARGET_DIR"
  cd "$TARGET_DIR" || {
    echo "错误：无法进入目录"
    exit 1
  }

  echo "正在执行 wire"
  wire

  echo "任务完成！"
)
