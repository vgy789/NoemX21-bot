package app

import (
	"testing"

	"github.com/vgy789/noemx21-bot/internal/transport/telegram/mock"
	"go.uber.org/mock/gomock"
)

func TestApp_Run(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockTG := mock.NewMockTelegramService(ctrl)

	// Expect Run to be called once
	mockTG.EXPECT().Run().Times(1)

	a := &App{
		tg: mockTG,
	}

	a.Run()
}
