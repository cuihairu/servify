import React, { useEffect, useRef, useState } from 'react';
import { useServify, useChat, useAI, useSatisfaction, useRemoteAssist } from '@servify/react';

const ChatDemo: React.FC = () => {
  const { isConnected } = useServify();
  const {
    session,
    messages,
    agent,
    isLoading,
    error,
    startChat,
    sendMessage,
    endChat,
    uploadFile,
  } = useChat();

  const { askAI } = useAI();
  const { submitRating } = useSatisfaction();
  const {
    state: remoteAssistState,
    isActive: remoteAssistActive,
    error: remoteAssistError,
    remoteStream,
    startRemoteAssist,
    endRemoteAssist,
  } = useRemoteAssist();

  const [messageText, setMessageText] = useState('');
  const fileInputRef = useRef<HTMLInputElement>(null);
  const remoteVideoRef = useRef<HTMLVideoElement>(null);

  useEffect(() => {
    if (!remoteVideoRef.current) {
      return;
    }

    remoteVideoRef.current.srcObject = remoteStream;
  }, [remoteStream]);

  const handleSendMessage = async () => {
    if (!messageText.trim() || !session) {
      return;
    }

    try {
      await sendMessage(messageText);
      setMessageText('');
    } catch (err) {
      window.alert(`Send message failed: ${(err as Error).message}`);
    }
  };

  const handleStartChat = async () => {
    try {
      await startChat({
        priority: 'normal',
        message: 'Hello, I need help with my current issue.',
      });
    } catch (err) {
      window.alert(`Start chat failed: ${(err as Error).message}`);
    }
  };

  const handleEndChat = async () => {
    try {
      await endRemoteAssist();
      await endChat();
    } catch (err) {
      window.alert(`End chat failed: ${(err as Error).message}`);
    }
  };

  const handleStartRemoteAssist = async () => {
    if (!session) {
      window.alert('Start a chat before opening remote assist.');
      return;
    }

    try {
      await startRemoteAssist({ captureScreen: true, audio: false });
    } catch (err) {
      window.alert(`Start remote assist failed: ${(err as Error).message}`);
    }
  };

  const handleStopRemoteAssist = async () => {
    try {
      await endRemoteAssist();
    } catch (err) {
      window.alert(`Stop remote assist failed: ${(err as Error).message}`);
    }
  };

  const handleAskAI = async () => {
    const question = window.prompt('Ask the AI assistant:');
    if (!question) {
      return;
    }

    try {
      const response = await askAI(question);
      window.alert(`AI answer (${(response.confidence * 100).toFixed(1)}%): ${response.answer}`);
    } catch (err) {
      window.alert(`AI request failed: ${(err as Error).message}`);
    }
  };

  const handleFileUpload = async (event: React.ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    if (!file || !session) {
      return;
    }

    try {
      const result = await uploadFile(file);
      window.alert(`Uploaded ${result.fileName} (${Math.round(result.fileSize / 1024)} KB)`);
    } catch (err) {
      window.alert(`File upload failed: ${(err as Error).message}`);
    } finally {
      event.target.value = '';
    }
  };

  const handleRating = async () => {
    const rating = window.prompt('Rate the support experience (1-5):');
    const ratingNum = Number.parseInt(rating || '', 10);

    if (!Number.isInteger(ratingNum) || ratingNum < 1 || ratingNum > 5) {
      window.alert('Enter an integer from 1 to 5.');
      return;
    }

    const comment = window.prompt('Optional comment:') || '';

    try {
      await submitRating({
        rating: ratingNum,
        comment,
      });
      window.alert('Thanks for the feedback.');
    } catch (err) {
      window.alert(`Submit rating failed: ${(err as Error).message}`);
    }
  };

  const handleKeyPress = (event: React.KeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'Enter' && !event.shiftKey && session) {
      event.preventDefault();
      void handleSendMessage();
    }
  };

  return (
    <>
      <div className="status-bar">
        <div style={{ display: 'flex', alignItems: 'center' }}>
          <div className={`status-dot ${isConnected ? 'connected' : 'disconnected'}`}></div>
          <span>{isConnected ? 'Realtime connected' : 'Realtime disconnected'}</span>
        </div>
        <div><strong>Contract:</strong> <code>ws://localhost:8080/api/v1/ws</code></div>
        {agent && (
          <div>
            Agent: <strong>{agent.name}</strong>
          </div>
        )}
      </div>

      <div className="chat-area">
        <div className="message system">
          This example uses the current WebSocket-first chat flow. Session history still reads from
          <code>/api/omni/sessions/:id/messages</code>.
        </div>

        {messages.map((message) => (
          <div key={String(message.id)} className={`message ${message.sender_type}`}>
            {message.content}
          </div>
        ))}

        {error && <div className="message system">Error: {error.message}</div>}
        <div className="message system">Remote assist state: {remoteAssistState}</div>
        {remoteAssistError && (
          <div className="message system">Remote assist error: {remoteAssistError.message}</div>
        )}
        <div className="message system">
          Remote media: {remoteStream ? 'stream attached' : 'waiting for remote track'}
        </div>
      </div>

      <div
        style={{
          margin: '12px 0',
          padding: 12,
          border: '1px solid #d9d9d9',
          borderRadius: 8,
          background: '#fafafa',
        }}
      >
        <div style={{ marginBottom: 8, fontSize: 14, color: '#555' }}>Remote assist preview</div>
        <video
          ref={remoteVideoRef}
          autoPlay
          playsInline
          muted
          style={{
            width: '100%',
            minHeight: 160,
            background: '#111',
            borderRadius: 6,
            display: remoteStream ? 'block' : 'none',
          }}
        />
        {!remoteStream && (
          <div
            style={{
              minHeight: 160,
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              color: '#888',
              background: '#111',
              borderRadius: 6,
            }}
          >
            No remote media stream yet.
          </div>
        )}
      </div>

      <div className="controls">
        <div className="input-group">
          <input
            type="text"
            value={messageText}
            onChange={(event) => setMessageText(event.target.value)}
            onKeyDown={handleKeyPress}
            placeholder="Type a message for the realtime channel..."
            maxLength={1000}
            disabled={!session}
          />
          <button
            className="btn secondary"
            onClick={() => fileInputRef.current?.click()}
            disabled={!session}
          >
            Upload
          </button>
          <button
            className="btn primary"
            onClick={handleSendMessage}
            disabled={!session || !messageText.trim() || isLoading}
          >
            Send
          </button>
        </div>

        <div>
          <button className="btn success" onClick={handleStartChat} disabled={!!session || isLoading}>
            Start chat
          </button>
          <button className="btn danger" onClick={handleEndChat} disabled={!session || isLoading}>
            End chat
          </button>
          <button className="btn secondary" onClick={handleAskAI} disabled={isLoading}>
            Ask AI
          </button>
          <button className="btn secondary" onClick={handleRating} disabled={!session}>
            Rate support
          </button>
          <button
            className="btn secondary"
            onClick={handleStartRemoteAssist}
            disabled={!session || remoteAssistActive}
          >
            Start remote assist
          </button>
          <button className="btn danger" onClick={handleStopRemoteAssist} disabled={!remoteAssistActive}>
            Stop remote assist
          </button>
        </div>

        <input
          ref={fileInputRef}
          type="file"
          className="file-input"
          onChange={handleFileUpload}
        />
      </div>
    </>
  );
};

export default ChatDemo;
