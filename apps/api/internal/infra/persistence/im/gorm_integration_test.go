package inframim

import (
	domainim "GCFeed/internal/domain/im"
	infraaccount "GCFeed/internal/infra/persistence/account"
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func TestConversationFlowIntegration(t *testing.T) {
	dsn := os.Getenv("IM_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("IM_TEST_MYSQL_DSN is not set")
	}
	db, err := gorm.Open(gormmysql.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open mysql: %v", err)
	}
	if err := db.AutoMigrate(&infraaccount.UserModel{}, &ConversationModel{}, &MemberModel{}, &MessageModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	suffix := fmt.Sprintf("_%d", time.Now().UnixNano())
	accountA := infraaccount.UserModel{
		Account:  "im_a" + suffix,
		Password: "hash",
		Nickname: "用户A",
		Role:     "USER",
		Status:   1,
	}
	accountB := infraaccount.UserModel{
		Account:  "im_b" + suffix,
		Password: "hash",
		Nickname: "用户B",
		Role:     "USER",
		Status:   1,
	}
	if err := db.Create(&accountA).Error; err != nil {
		t.Fatalf("create account A: %v", err)
	}
	if err := db.Create(&accountB).Error; err != nil {
		t.Fatalf("create account B: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM im_message WHERE conversation_id IN (SELECT id FROM im_conversation WHERE direct_key = ?)", domainim.DirectKey(accountA.ID, accountB.ID))
		db.Exec("DELETE FROM im_conversation_member WHERE conversation_id IN (SELECT id FROM im_conversation WHERE direct_key = ?)", domainim.DirectKey(accountA.ID, accountB.ID))
		db.Exec("DELETE FROM im_conversation WHERE direct_key = ?", domainim.DirectKey(accountA.ID, accountB.ID))
		db.Delete(&accountA)
		db.Delete(&accountB)
	})

	conversation, members, err := domainim.NewDirectConversation(accountA.ID, accountB.ID)
	if err != nil {
		t.Fatalf("new direct conversation: %v", err)
	}
	repo := New(db)
	savedConversation, created, err := repo.CreateConversation(context.Background(), conversation, members)
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	if !created || savedConversation.ID <= 0 {
		t.Fatalf("unexpected conversation: %+v created=%v", savedConversation, created)
	}

	message, err := domainim.NewTextMessage(savedConversation.ID, accountA.ID, "集成测试消息", "client-integration-1")
	if err != nil {
		t.Fatalf("new message: %v", err)
	}
	savedMessage, recipients, created, err := repo.SendMessage(context.Background(), message)
	if err != nil {
		t.Fatalf("send message: %v", err)
	}
	if !created || savedMessage.Sequence != 1 || len(recipients) != 2 {
		t.Fatalf("unexpected message: %+v recipients=%v created=%v", savedMessage, recipients, created)
	}

	replayed, _, created, err := repo.SendMessage(context.Background(), message)
	if err != nil {
		t.Fatalf("replay message: %v", err)
	}
	if created || replayed.ID != savedMessage.ID {
		t.Fatalf("message replay was not idempotent: %+v created=%v", replayed, created)
	}

	messages, err := repo.ListMessages(context.Background(), savedConversation.ID, 0, 20)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(messages) != 1 || messages[0].Content != "集成测试消息" || messages[0].SenderNickname != "用户A" {
		t.Fatalf("unexpected messages: %+v", messages)
	}
}
