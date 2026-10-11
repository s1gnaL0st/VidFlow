import { useCallback, useEffect, useMemo, useRef, useState } from "react";

const fallbackAvatar =
  "https://lh3.googleusercontent.com/aida-public/AB6AXuAEH5ZNpPdQoO7Qiy3CGshEypK0dp1HFeVoZ1TAHDLhfcvYMg_js-k2rhBTIPpqMjs6GQpIgKMnUhIu0tY_QYUCTocPD70FDbGWYmHI25NPQ1Quod_7Ssaq0ICv7vvwNephDLNouriPhG7ubVy8GZKbFP9Qi-2yLe2mzST0t9Vfygo2jvgiHITh11LVRZ2ZTcE3nmySn6ZMnpqONtz0zbaKbQMLsDNfR-5smwYHCQLvdp6U5U2-OW_kZS1P6U9vR_PN9Ey84a1VDgRZ";

function ChatPage({ route, session, onNavigate }) {
  const [conversations, setConversations] = useState([]);
  const [selectedID, setSelectedID] = useState(() => conversationIDFromRoute(route));
  const [messages, setMessages] = useState([]);
  const [draft, setDraft] = useState("");
  const [loading, setLoading] = useState(true);
  const [sending, setSending] = useState(false);
  const [error, setError] = useState("");
  const [groupOpen, setGroupOpen] = useState(false);
  const [groupTitle, setGroupTitle] = useState("");
  const [groupMembers, setGroupMembers] = useState("");
  const [creatingGroup, setCreatingGroup] = useState(false);
  const socketRef = useRef(null);
  const reconnectTimerRef = useRef(null);
  const selectedIDRef = useRef(selectedID);
  const bottomRef = useRef(null);

  useEffect(() => {
    selectedIDRef.current = selectedID;
  }, [selectedID]);

  const loadConversations = useCallback(async () => {
    if (!session.token) return;
    const data = await apiRequest("/api/conversations", { token: session.token });
    setConversations(Array.isArray(data.items) ? data.items : []);
  }, [session.token]);

  const loadMessages = useCallback(
    async (conversationID) => {
      if (!session.token || !conversationID) {
        setMessages([]);
        return;
      }
      const data = await apiRequest(`/api/conversations/${conversationID}/messages?limit=50`, {
        token: session.token
      });
      setMessages([...(data.items || [])].reverse());
    },
    [session.token]
  );

  useEffect(() => {
    let active = true;
    setLoading(true);
    setError("");
    Promise.all([loadConversations(), selectedID ? loadMessages(selectedID) : Promise.resolve()])
      .catch((loadError) => {
        if (active) setError(loadError.message || "私信加载失败");
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [loadConversations, loadMessages, selectedID]);

  useEffect(() => {
    if (!route.startsWith("/chat/new/")) return;
    const targetUserID = Number(route.slice("/chat/new/".length));
    if (!Number.isFinite(targetUserID) || targetUserID <= 0 || !session.token) return;
    let active = true;
    apiRequest("/api/conversations", {
      method: "POST",
      token: session.token,
      body: { type: "DIRECT", target_user_id: targetUserID }
    })
      .then((data) => {
        const conversationID = Number(data.conversation?.id || 0);
        if (active && conversationID > 0) {
          onNavigate(`/chat/${conversationID}`);
        }
      })
      .catch((createError) => {
        if (active) setError(createError.message || "创建会话失败");
      });
    return () => {
      active = false;
    };
  }, [onNavigate, route, session.token]);

  useEffect(() => {
    const routeConversationID = conversationIDFromRoute(route);
    if (routeConversationID > 0) {
      setSelectedID(routeConversationID);
    }
  }, [route]);

  useEffect(() => {
    if (!session.token) return undefined;
    let closed = false;
    let retry = 0;

    function connect() {
      if (closed) return;
      const scheme = window.location.protocol === "https:" ? "wss" : "ws";
      const socket = new WebSocket(
        `${scheme}://${window.location.host}/api/im/ws?token=${encodeURIComponent(session.token)}`
      );
      socketRef.current = socket;
      socket.onopen = () => {
        retry = 0;
      };
      socket.onmessage = (event) => {
        let payload;
        try {
          payload = JSON.parse(event.data);
        } catch {
          return;
        }
        if (payload.type !== "message.created" || !payload.data) return;
        const message = payload.data;
        setConversations((items) => {
          const next = items.map((item) => {
            if (Number(item.id) !== Number(message.conversation_id)) return item;
            const unread =
              Number(message.conversation_id) === Number(selectedIDRef.current)
                ? 0
                : Number(item.unread_count || 0) + (Number(message.sender_id) === Number(session.user?.id) ? 0 : 1);
            return { ...item, last_message: message, unread_count: unread };
          });
          const selected = next.find((item) => Number(item.id) === Number(message.conversation_id));
          return selected
            ? [selected, ...next.filter((item) => Number(item.id) !== Number(message.conversation_id))]
            : [
                {
                  id: message.conversation_id,
                  type: "DIRECT",
                  title: "",
                  members: [],
                  unread_count: 1,
                  last_message: message
                },
                ...next
              ];
        });
        if (Number(message.conversation_id) === Number(selectedIDRef.current)) {
          setMessages((items) => appendUniqueMessage(items, message));
          if (Number(message.sender_id) !== Number(session.user?.id)) {
            markConversationRead(session.token, message.conversation_id, message.sequence).catch(() => {});
          }
        }
      };
      socket.onclose = () => {
        if (closed) return;
        retry += 1;
        reconnectTimerRef.current = window.setTimeout(connect, Math.min(1000 * 2 ** retry, 15000));
      };
    }

    connect();
    return () => {
      closed = true;
      if (reconnectTimerRef.current) window.clearTimeout(reconnectTimerRef.current);
      socketRef.current?.close();
    };
  }, [session.token, session.user?.id]);

  useEffect(() => {
    if (!selectedID || !session.token) return;
    const last = messages[messages.length - 1];
    if (last?.sequence) {
      markConversationRead(session.token, selectedID, last.sequence).catch(() => {});
    }
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messages, selectedID, session.token]);

  const selectedConversation = useMemo(
    () => conversations.find((item) => Number(item.id) === Number(selectedID)),
    [conversations, selectedID]
  );

  async function sendMessage(event) {
    event.preventDefault();
    const content = draft.trim();
    if (!content || !selectedID || !session.token || sending) return;
    setSending(true);
    setError("");
    try {
      const message = await apiRequest(`/api/conversations/${selectedID}/messages`, {
        method: "POST",
        token: session.token,
        body: {
          type: "TEXT",
          content,
          client_message_id: createClientMessageID()
        }
      });
      setDraft("");
      setMessages((items) => appendUniqueMessage(items, message));
      await loadConversations();
    } catch (sendError) {
      setError(sendError.message || "消息发送失败");
    } finally {
      setSending(false);
    }
  }

  async function createGroup(event) {
    event.preventDefault();
    const title = groupTitle.trim();
    const memberIDs = groupMembers
      .split(/[,\s]+/)
      .map((value) => Number(value.trim()))
      .filter((value) => Number.isFinite(value) && value > 0);
    if (!title || memberIDs.length === 0 || creatingGroup) return;
    setCreatingGroup(true);
    setError("");
    try {
      const data = await apiRequest("/api/conversations", {
        method: "POST",
        token: session.token,
        body: {
          type: "GROUP",
          title,
          member_ids: memberIDs
        }
      });
      const conversationID = Number(data.conversation?.id || 0);
      setGroupOpen(false);
      setGroupTitle("");
      setGroupMembers("");
      await loadConversations();
      if (conversationID > 0) onNavigate(`/chat/${conversationID}`);
    } catch (createError) {
      setError(createError.message || "创建群聊失败");
    } finally {
      setCreatingGroup(false);
    }
  }

  return (
    <main className="chat-page">
      <section className="chat-sidebar">
        <div className="chat-sidebar-head">
          <div>
            <span className="section-eyebrow">Instant Messaging</span>
            <h1>私信</h1>
          </div>
          <span className="chat-sidebar-actions">
            <button className="icon-button" type="button" onClick={() => setGroupOpen(true)} title="创建群聊">
              <span className="material-symbols-outlined">group_add</span>
            </button>
            <button className="icon-button" type="button" onClick={loadConversations} title="刷新会话">
              <span className="material-symbols-outlined">refresh</span>
            </button>
          </span>
        </div>
        <div className="conversation-list">
          {loading && <p className="chat-muted">加载中...</p>}
          {!loading && conversations.length === 0 && <p className="chat-muted">暂无会话，从用户主页发起私信。</p>}
          {conversations.map((conversation) => (
            <button
              className={`conversation-item ${Number(conversation.id) === Number(selectedID) ? "active" : ""}`}
              key={conversation.id}
              type="button"
              onClick={() => onNavigate(`/chat/${conversation.id}`)}
            >
              <ConversationAvatar conversation={conversation} currentUserID={session.user?.id} />
              <span className="conversation-copy">
                <span className="conversation-title-row">
                  <strong>{conversationTitle(conversation, session.user?.id)}</strong>
                  <small>{formatChatTime(conversation.last_message?.created_at || conversation.updated_at)}</small>
                </span>
                <span className="conversation-preview">
                  {conversation.last_message ? messagePreview(conversation.last_message) : "开始聊天"}
                </span>
              </span>
              {Number(conversation.unread_count || 0) > 0 && (
                <span className="conversation-unread">{Math.min(Number(conversation.unread_count), 99)}</span>
              )}
            </button>
          ))}
        </div>
      </section>

      <section className="chat-panel">
        {selectedID ? (
          <>
            <header className="chat-panel-head">
              <div>
                <h2>{conversationTitle(selectedConversation, session.user?.id)}</h2>
                <span>{selectedConversation?.members?.some((member) => member.online) ? "在线" : "离线消息"}</span>
              </div>
              <button className="icon-button" type="button" onClick={() => loadMessages(selectedID)} title="刷新消息">
                <span className="material-symbols-outlined">refresh</span>
              </button>
            </header>
            <div className="chat-messages">
              {messages.map((message) => {
                const mine = Number(message.sender_id) === Number(session.user?.id);
                return (
                  <article className={`chat-bubble-row ${mine ? "mine" : ""}`} key={`${message.id}-${message.sequence}`}>
                    {!mine && <img src={message.sender_avatar_url || fallbackAvatar} alt="" />}
                    <div className="chat-bubble">
                      <p>{messagePreview(message)}</p>
                      <span>{formatChatTime(message.created_at)}</span>
                    </div>
                  </article>
                );
              })}
              <div ref={bottomRef} />
            </div>
            {error && <p className="form-message">{error}</p>}
            <form className="chat-composer" onSubmit={sendMessage}>
              <textarea
                value={draft}
                onChange={(event) => setDraft(event.target.value)}
                placeholder="输入消息"
                maxLength={4000}
                onKeyDown={(event) => {
                  if (event.key === "Enter" && !event.shiftKey) {
                    event.preventDefault();
                    sendMessage(event);
                  }
                }}
              />
              <button className="primary-button" type="submit" disabled={!draft.trim() || sending}>
                <span className="material-symbols-outlined">send</span>
                {sending ? "发送中" : "发送"}
              </button>
            </form>
          </>
        ) : (
          <div className="chat-empty">
            <span className="material-symbols-outlined">forum</span>
            <h2>选择一个会话</h2>
            <p>支持文本消息和后续的视频卡片分享。</p>
          </div>
        )}
      </section>
      {groupOpen && (
        <div className="modal-backdrop" role="presentation" onClick={() => setGroupOpen(false)}>
          <form className="chat-group-modal" onSubmit={createGroup} onClick={(event) => event.stopPropagation()}>
            <header>
              <h2>创建群聊</h2>
              <button className="icon-button" type="button" onClick={() => setGroupOpen(false)}>
                <span className="material-symbols-outlined">close</span>
              </button>
            </header>
            <label>
              群聊名称
              <input value={groupTitle} onChange={(event) => setGroupTitle(event.target.value)} maxLength={128} />
            </label>
            <label>
              成员用户 ID
              <textarea
                value={groupMembers}
                onChange={(event) => setGroupMembers(event.target.value)}
                placeholder="例如 12, 18, 25"
              />
            </label>
            {error && <p className="form-message">{error}</p>}
            <button className="primary-button" type="submit" disabled={creatingGroup || !groupTitle.trim()}>
              {creatingGroup ? "创建中" : "创建群聊"}
            </button>
          </form>
        </div>
      )}
    </main>
  );
}

function ConversationAvatar({ conversation, currentUserID }) {
  const peers = (conversation.members || []).filter((member) => Number(member.user_id) !== Number(currentUserID));
  const member = peers[0] || conversation.members?.[0];
  return <img className="conversation-avatar" src={member?.avatar_url || fallbackAvatar} alt="" />;
}

function conversationTitle(conversation, currentUserID) {
  if (!conversation) return "私信";
  if (conversation.title) return conversation.title;
  const peers = (conversation.members || []).filter((member) => Number(member.user_id) !== Number(currentUserID));
  return peers.map((member) => member.nickname || `用户_${member.user_id}`).join("、") || `会话 ${conversation.id}`;
}

function messagePreview(message) {
  if (!message) return "";
  if (message.type === "VIDEO") return `[视频] ${message.content || `#${message.video_id}`}`;
  return message.content || "";
}

function conversationIDFromRoute(route) {
  const match = /^\/chat\/(\d+)$/.exec(route);
  return match ? Number(match[1]) : 0;
}

function createClientMessageID() {
  if (window.crypto?.randomUUID) return window.crypto.randomUUID();
  return `${Date.now()}-${Math.random().toString(16).slice(2)}`;
}

function appendUniqueMessage(items, message) {
  const exists = items.some(
    (item) =>
      Number(item.id) === Number(message.id) ||
      (message.client_message_id && item.client_message_id === message.client_message_id)
  );
  return exists ? items : [...items, message];
}

async function markConversationRead(token, conversationID, sequence) {
  return apiRequest(`/api/conversations/${conversationID}/read`, {
    method: "PATCH",
    token,
    body: { sequence }
  });
}

function formatChatTime(value) {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  const now = new Date();
  if (date.toDateString() === now.toDateString()) {
    return date.toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" });
  }
  return date.toLocaleDateString("zh-CN", { month: "2-digit", day: "2-digit" });
}

async function apiRequest(path, options = {}) {
  const headers = { Accept: "application/json" };
  if (options.body) headers["Content-Type"] = "application/json";
  if (options.token) headers.Authorization = `Bearer ${options.token}`;
  const response = await fetch(path, {
    method: options.method || "GET",
    headers,
    body: options.body ? JSON.stringify(options.body) : undefined
  });
  const data = await response.json().catch(() => ({}));
  if (!response.ok) {
    const error = new Error(data.error || data.message || response.statusText || "请求失败");
    error.status = response.status;
    throw error;
  }
  return data;
}

export default ChatPage;
