import React, { useEffect, useMemo, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import "./styles.css";

const apiBase = import.meta.env.VITE_API_BASE || "/api";
const wsUrl =
  import.meta.env.VITE_WS_URL ||
  `${window.location.protocol === "https:" ? "wss" : "ws"}://${window.location.host}${import.meta.env.VITE_WS_PATH || "/ws"}`;

function App() {
  const [messages, setMessages] = useState([]);
  const [author, setAuthor] = useState("Student");
  const [text, setText] = useState("");
  const [status, setStatus] = useState("connecting");
  const listRef = useRef(null);

  useEffect(() => {
    fetch(`${apiBase}/messages`)
      .then((response) => response.json())
      .then((data) => setMessages(data.messages || []))
      .catch(() => setStatus("api unavailable"));
  }, []);

  useEffect(() => {
    let closed = false;
    let retryTimer;

    const connect = () => {
      const socket = new WebSocket(wsUrl);
      socket.addEventListener("open", () => setStatus("live"));
      socket.addEventListener("message", (event) => {
        try {
          const message = JSON.parse(event.data);
          if (message.id) {
            setMessages((current) => [...current.filter((item) => item.id !== message.id), message]);
          }
        } catch {
          // Ignore non-message control frames.
        }
      });
      socket.addEventListener("close", () => {
        if (!closed) {
          setStatus("reconnecting");
          retryTimer = window.setTimeout(connect, 1500);
        }
      });
      socket.addEventListener("error", () => setStatus("connection issue"));
    };

    connect();
    return () => {
      closed = true;
      window.clearTimeout(retryTimer);
    };
  }, []);

  useEffect(() => {
    listRef.current?.scrollTo({ top: listRef.current.scrollHeight, behavior: "smooth" });
  }, [messages.length]);

  const orderedMessages = useMemo(
    () => [...messages].sort((a, b) => Number(a.id) - Number(b.id)),
    [messages]
  );

  async function sendMessage(event) {
    event.preventDefault();
    const trimmed = text.trim();
    if (!trimmed) {
      return;
    }
    const response = await fetch(`${apiBase}/messages`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ author, text: trimmed })
    });
    if (!response.ok) {
      setStatus("send failed");
      return;
    }
    setText("");
  }

  return (
    <main className="shell">
      <section className="hero">
        <div>
          <p className="eyebrow">tlfpaas demo</p>
          <h1>Messenger Stack</h1>
          <p className="lede">
            React, Go, WebSockets, NATS JetStream, Redis and Postgres in one deployable project.
          </p>
        </div>
        <span className={`status status-${status.replaceAll(" ", "-")}`}>{status}</span>
      </section>

      <section className="chat">
        <div className="messages" ref={listRef} aria-live="polite">
          {orderedMessages.length === 0 ? (
            <p className="empty">No messages yet. Send the first one.</p>
          ) : (
            orderedMessages.map((message) => (
              <article className="message" key={message.id}>
                <header>
                  <strong>{message.author}</strong>
                  <time>{new Date(message.created_at).toLocaleTimeString()}</time>
                </header>
                <p>{message.text}</p>
              </article>
            ))
          )}
        </div>

        <form className="composer" onSubmit={sendMessage}>
          <label>
            Name
            <input value={author} maxLength={80} onChange={(event) => setAuthor(event.target.value)} />
          </label>
          <label>
            Message
            <textarea value={text} maxLength={1000} onChange={(event) => setText(event.target.value)} />
          </label>
          <button type="submit">Send message</button>
        </form>
      </section>
    </main>
  );
}

createRoot(document.getElementById("root")).render(<App />);

