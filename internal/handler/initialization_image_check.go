package handler

import (
	"context"
	"fmt"
	"net/http"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/imagegen"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/utils"
	"github.com/gin-gonic/gin"
)

const imageGenerationCheckPrompt = "a small red circle on a white background"

// CheckImageGenerationModel godoc
// @Summary      检查图像生成模型
// @Description  检查图像生成模型连接是否正常，通过生成一张小图测试 /images/generations 端点
// @Tags         初始化
// @Accept       json
// @Produce      json
// @Param        request  body      handler.ModelTestRequest  true  "图像生成模型检查请求"
// @Success      200      {object}  map[string]interface{}  "检查结果"
// @Failure      400      {object}  errors.AppError         "请求参数错误"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /initialization/image-generation/check [post]
func (h *InitializationHandler) CheckImageGenerationModel(c *gin.Context) {
	ctx := c.Request.Context()

	logger.Info(ctx, "Checking image generation model connection")

	var req ModelTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "Failed to parse image generation model check request", err)
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	h.fillSecretsFromStoredModel(ctx, &req)

	if req.ModelName == "" || req.BaseURL == "" {
		logger.Error(ctx, "Model name and base URL are required for image generation check")
		c.Error(errors.NewBadRequestError("模型名称和Base URL不能为空"))
		return
	}

	if err := utils.ValidateURLForSSRF(req.BaseURL); err != nil {
		logger.Warnf(ctx, "SSRF validation failed for image generation BaseURL: %v", err)
		c.Error(errors.NewBadRequestError(utils.FormatSSRFError("Base URL", req.BaseURL, err)))
		return
	}

	model := h.buildTestModel(&req, types.ModelTypeImageGeneration, types.ModelSourceRemote)
	available, message := checkImageGenerationConnection(ctx, model)

	logger.Infof(ctx, "Image generation model check completed, available: %v, message: %s", available, message)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"available": available,
			"message":   message,
		},
	})
}

// checkImageGenerationConnection generates one image with a neutral prompt at
// the smallest size the model documents.
func checkImageGenerationConnection(ctx context.Context, model *types.Model) (bool, string) {
	generator, err := imagegen.New(imagegen.ConfigFromModel(model))
	if err != nil {
		logger.Errorf(ctx, "Failed to create image generation instance for check: %v", err)
		return false, fmt.Sprintf("创建图像生成实例失败: %v", err)
	}

	res, err := generator.Generate(ctx, imagegen.Request{
		Prompt: imageGenerationCheckPrompt,
		Count:  1,
		Size:   imagegen.CheckSize(generator),
	})
	if err != nil {
		errMsg := err.Error()
		if hint, unreachable := modelCheckFailureHint(errMsg); unreachable {
			return false, fmt.Sprintf("%s：%s", hint, errMsg)
		}
		logger.Infof(ctx, "Image generation check got non-fatal error (endpoint reachable): %v", err)
		return true, fmt.Sprintf("图像生成端点可达（非致命错误: %s）", errMsg)
	}
	if res == nil || len(res.Images) == 0 {
		return true, "图像生成端点可达，但未返回图片"
	}
	return true, fmt.Sprintf("图像生成连接成功，已生成 1 张图片（%d 字节）", len(res.Images[0].Data))
}
