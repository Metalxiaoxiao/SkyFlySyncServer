package fileserver

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"logger"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
)

const uploadDirectory = "./uploads"

// getFileHash 返回给定文件名的SHA-256哈希。
func getFileHash(fileName string) (string, error) {
	file, err := os.Open(fileName)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// CopyAndRenameFile 复制并重命名文件
func CopyAndRenameFile(filePath, newFilePath string) error {
	srcFile, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(newFilePath)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	if err != nil {
		return err
	}

	return nil
}

// HandleFileUpload 处理文件上传
func HandleFileUpload(w http.ResponseWriter, r *http.Request) {
	file, handler, err := r.FormFile("file")
	if err != nil {
		logger.Error("无法获取文件: %v", err)
		http.Error(w, "无法获取文件", http.StatusBadRequest)
		return
	}
	defer func(file multipart.File) {
		err := file.Close()
		if err != nil {
			logger.Error(err)
		}
	}(file)

	if err := os.MkdirAll(uploadDirectory, os.ModePerm); err != nil {
		logger.Error("创建上传目录时发生错误: %v", err)
		http.Error(w, "创建上传目录时发生错误", http.StatusInternalServerError)
		return
	}

	filePath := filepath.Join(uploadDirectory, handler.Filename)
	dst, err := os.Create(filePath)
	if err != nil {
		logger.Error("在服务器上创建文件时发生错误: %v", err)
		http.Error(w, "在服务器上创建文件时发生错误", http.StatusInternalServerError)
		return
	}
	defer func(dst *os.File) {
		err := dst.Close()
		if err != nil {
			logger.Error(err)
		}
	}(dst)

	if _, err := io.Copy(dst, file); err != nil {
		logger.Error("将文件内容复制到服务器上的文件时发生错误: %v", err)
		http.Error(w, "将文件内容复制到服务器上的文件时发生错误", http.StatusInternalServerError)
		return
	}

	file, err = os.Open(filePath)
	if err != nil {
		logger.Error("重新打开文件时发生错误: %v", err)
		http.Error(w, "重新打开文件时发生错误", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		logger.Error("计算文件哈希时发生错误: %v", err)
		http.Error(w, "计算文件哈希时发生错误", http.StatusInternalServerError)
		return
	}

	hashInBytes := hasher.Sum(nil)[:]
	sha256Hash := fmt.Sprintf("%x", hashInBytes)

	newFileName := sha256Hash
	newFilePath := filepath.Join(uploadDirectory, newFileName)

	file.Close()
	if err := CopyAndRenameFile(filePath, newFilePath); err != nil {
		logger.Error("重命名文件时发生错误: %v", err)
		http.Error(w, "重命名文件时发生错误", http.StatusInternalServerError)
		return
	}

	_, err = fmt.Fprintf(w, "文件 %s 上传成功", newFileName)
	if err != nil {
		logger.Error("写入成功响应时发生错误: %v", err)
	}
}

// HandleFileDownload 处理文件下载
func HandleFileDownload(w http.ResponseWriter, r *http.Request) {
	fileName := filepath.Base(r.URL.Path)
	filePath := filepath.Join(uploadDirectory, fileName)

	file, err := os.Open(filePath)
	if err != nil {
		logger.Error("未找到文件: %v", err)
		http.Error(w, "未找到文件", http.StatusNotFound)
		return
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			logger.Error(err)
		}
	}(file)

	w.Header().Set("Content-Type", "application/octet-stream")

	_, err = io.Copy(w, file)
	if err != nil {
		logger.Error("将文件内容复制到响应时发生错误: %v", err)
		http.Error(w, "将文件内容复制到响应时发生错误", http.StatusInternalServerError)
		return
	}
}
