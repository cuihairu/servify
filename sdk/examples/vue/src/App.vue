<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import {
  useServifyReady,
  useChat,
  useAI,
  useSatisfaction,
  useRemoteAssist,
} from '@servify/vue';

const { isConnected } = useServifyReady();
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

const messageText = ref('');
const remoteVideo = ref<HTMLVideoElement | null>(null);

watch(remoteStream, (stream) => {
  if (remoteVideo.value) {
    remoteVideo.value.srcObject = stream;
  }
});

const remoteMediaLabel = computed(() => (remoteStream.value ? 'stream attached' : 'waiting for remote track'));

async function handleStartChat() {
  try {
    await startChat({
      priority: 'normal',
      message: 'Hello, I need help with my current issue.',
    });
  } catch (err) {
    window.alert(`Start chat failed: ${(err as Error).message}`);
  }
}

async function handleSendMessage() {
  if (!session.value || !messageText.value.trim()) {
    return;
  }

  try {
    await sendMessage(messageText.value);
    messageText.value = '';
  } catch (err) {
    window.alert(`Send message failed: ${(err as Error).message}`);
  }
}

async function handleEndChat() {
  try {
    await endRemoteAssist();
    await endChat();
  } catch (err) {
    window.alert(`End chat failed: ${(err as Error).message}`);
  }
}

async function handleAskAI() {
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
}

async function handleRating() {
  const raw = window.prompt('Rate the support experience (1-5):');
  const rating = Number(raw);
  if (!Number.isInteger(rating) || rating < 1 || rating > 5) {
    window.alert('Enter an integer from 1 to 5.');
    return;
  }

  const comment = window.prompt('Optional comment:') || '';

  try {
    await submitRating({ rating, comment });
    window.alert('Thanks for the feedback.');
  } catch (err) {
    window.alert(`Submit rating failed: ${(err as Error).message}`);
  }
}

async function handleStartRemoteAssist() {
  if (!session.value) {
    window.alert('Start a chat before opening remote assist.');
    return;
  }

  try {
    await startRemoteAssist({ captureScreen: true, audio: false });
  } catch (err) {
    window.alert(`Start remote assist failed: ${(err as Error).message}`);
  }
}

async function handleStopRemoteAssist() {
  try {
    await endRemoteAssist();
  } catch (err) {
    window.alert(`Stop remote assist failed: ${(err as Error).message}`);
  }
}

async function handleFileChange(event: Event) {
  const input = event.target as HTMLInputElement;
  const file = input.files?.[0];
  if (!file || !session.value) {
    return;
  }

  try {
    const result = await uploadFile(file);
    window.alert(`Uploaded ${result.fileName} (${Math.round(result.fileSize / 1024)} KB)`);
  } catch (err) {
    window.alert(`File upload failed: ${(err as Error).message}`);
  } finally {
    input.value = '';
  }
}
</script>

<template>
  <div class="container">
    <header class="header">
      <h1>Servify</h1>
      <p>Vue SDK example aligned with the current WebSocket-first contract.</p>
    </header>

    <section class="status-bar">
      <div class="status-left">
        <span class="status-dot" :class="isConnected ? 'connected' : 'disconnected'"></span>
        <span>{{ isConnected ? 'Realtime connected' : 'Realtime disconnected' }}</span>
      </div>
      <div><strong>Contract:</strong> <code>ws://localhost:8080/api/v1/ws</code></div>
      <div v-if="agent">Agent: <strong>{{ agent.name }}</strong></div>
    </section>

    <section class="chat-area">
      <div class="message system">
        This example uses the current WebSocket-first chat flow. Session history still reads from
        <code>/api/omni/sessions/:id/messages</code>.
      </div>

      <div v-for="message in messages" :key="String(message.id)" class="message" :class="message.sender_type">
        {{ message.content }}
      </div>

      <div v-if="error" class="message system">Error: {{ error.message }}</div>
      <div class="message system">Remote assist state: {{ remoteAssistState }}</div>
      <div v-if="remoteAssistError" class="message system">Remote assist error: {{ remoteAssistError.message }}</div>
      <div class="message system">Remote media: {{ remoteMediaLabel }}</div>
    </section>

    <section class="remote-preview">
      <div class="preview-title">Remote assist preview</div>
      <video
        v-show="remoteStream"
        ref="remoteVideo"
        autoplay
        playsinline
        muted
        class="preview-video"
      />
      <div v-if="!remoteStream" class="preview-empty">No remote media stream yet.</div>
    </section>

    <section class="controls">
      <div class="input-group">
        <input
          v-model="messageText"
          type="text"
          maxlength="1000"
          placeholder="Type a message for the realtime channel..."
          :disabled="!session"
          @keydown.enter.prevent="handleSendMessage"
        />
        <label class="btn secondary">
          Upload
          <input type="file" class="hidden-input" @change="handleFileChange" />
        </label>
        <button class="btn primary" :disabled="!session || !messageText.trim() || isLoading" @click="handleSendMessage">
          Send
        </button>
      </div>

      <div class="button-row">
        <button class="btn success" :disabled="!!session || isLoading" @click="handleStartChat">Start chat</button>
        <button class="btn danger" :disabled="!session || isLoading" @click="handleEndChat">End chat</button>
        <button class="btn secondary" :disabled="isLoading" @click="handleAskAI">Ask AI</button>
        <button class="btn secondary" :disabled="!session" @click="handleRating">Rate support</button>
        <button class="btn secondary" :disabled="!session || remoteAssistActive" @click="handleStartRemoteAssist">
          Start remote assist
        </button>
        <button class="btn danger" :disabled="!remoteAssistActive" @click="handleStopRemoteAssist">
          Stop remote assist
        </button>
      </div>
    </section>
  </div>
</template>
