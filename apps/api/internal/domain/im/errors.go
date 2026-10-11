package domainim

import "errors"

var ErrInvalidUserID = errors.New("invalid user id")
var ErrInvalidConversationID = errors.New("invalid conversation id")
var ErrInvalidMessageID = errors.New("invalid message id")
var ErrInvalidVideoID = errors.New("invalid video id")
var ErrInvalidConversationType = errors.New("invalid conversation type")
var ErrInvalidMessageType = errors.New("invalid message type")
var ErrEmptyTitle = errors.New("conversation title is required")
var ErrTitleTooLong = errors.New("conversation title is too long")
var ErrEmptyContent = errors.New("message content is required")
var ErrContentTooLong = errors.New("message content is too long")
var ErrClientMessageIDTooLong = errors.New("client message id is too long")
var ErrInvalidLimit = errors.New("invalid limit")
var ErrInvalidSequence = errors.New("invalid sequence")
var ErrConversationNotFound = errors.New("conversation not found")
var ErrMessageNotFound = errors.New("message not found")
var ErrUserNotFound = errors.New("user not found")
var ErrNotConversationMember = errors.New("not conversation member")
var ErrDirectConversationWithSelf = errors.New("direct conversation with self is not allowed")
