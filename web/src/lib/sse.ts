export interface SSEConversationEvent {
  id: string;
}

export interface SSETokenEvent {
  text: string;
}

export interface SSESentenceEvent {
  text: string;
}

export interface SSEToolCallEvent {
  id: string;
  name: string;
  arguments: string | Record<string, unknown>;
}

export interface SSEToolResultEvent {
  id: string;
  name: string;
  result: unknown;
  error?: string;
}

export interface SSEDoneEvent {
  usage?: {
    prompt_tokens?: number;
    completion_tokens?: number;
    total_tokens?: number;
    [key: string]: unknown;
  };
  timings?: {
    first_token_ms: number;
    total_ms: number;
  };
}

export interface SSEErrorEvent {
  message: string;
}

export type SSEEventMap = {
  conversation: SSEConversationEvent;
  token: SSETokenEvent;
  sentence: SSESentenceEvent;
  tool_call: SSEToolCallEvent;
  tool_result: SSEToolResultEvent;
  done: SSEDoneEvent;
  error: SSEErrorEvent;
};

export type SSEEventName = keyof SSEEventMap;

export interface SSEChatCallbacks {
  onConversation?: (data: SSEConversationEvent) => void;
  onToken?: (data: SSETokenEvent) => void;
  onSentence?: (data: SSESentenceEvent) => void;
  onToolCall?: (data: SSEToolCallEvent) => void;
  onToolResult?: (data: SSEToolResultEvent) => void;
  onDone?: (data: SSEDoneEvent) => void;
  onError?: (data: SSEErrorEvent) => void;
}

export interface StreamChatOptions {
  conversationId?: string;
  message: string;
  signal?: AbortSignal;
  callbacks: SSEChatCallbacks;
  mock?: boolean;
}

/**
 * Parses raw SSE chunks into event/data pairs.
 */
export async function streamChat({
  conversationId,
  message,
  signal,
  callbacks,
  mock = false,
}: StreamChatOptions): Promise<void> {
  if (mock) {
    return simulateMockStreamChat({ conversationId, message, signal, callbacks });
  }

  // HARD REQUIREMENT: fetch with relative path (no leading slash)
  const response = await fetch('sse/chat', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Accept: 'text/event-stream',
    },
    body: JSON.stringify({
      conversation_id: conversationId,
      message,
    }),
    signal,
  });

  if (!response.ok) {
    let errMessage = `HTTP ${response.status}: ${response.statusText}`;
    try {
      const errJson = await response.json();
      if (errJson?.error || errJson?.message) {
        errMessage = errJson.error || errJson.message;
      }
    } catch {
      // ignore json parse error
    }
    callbacks.onError?.({ message: errMessage });
    throw new Error(errMessage);
  }

  if (!response.body) {
    throw new Error('未接收到服务端响应流');
  }

  const reader = response.body.getReader();
  const decoder = new TextDecoder('utf-8');
  let buffer = '';
  let currentEvent: SSEEventName | string = 'token';
  let currentDataLines: string[] = [];

  const dispatchCurrentEvent = () => {
    if (currentDataLines.length === 0) return;
    const rawData = currentDataLines.join('\n').trim();
    currentDataLines = [];

    if (!rawData) return;

    let parsed: any;
    try {
      parsed = JSON.parse(rawData);
    } catch {
      // If not valid JSON, treat as string or wrap
      parsed = { text: rawData, message: rawData };
    }

    const eventName = currentEvent;
    // reset event to default token
    currentEvent = 'token';

    switch (eventName) {
      case 'conversation':
        callbacks.onConversation?.(parsed);
        break;
      case 'token':
        callbacks.onToken?.(parsed);
        break;
      case 'sentence':
        callbacks.onSentence?.(parsed);
        break;
      case 'tool_call':
        callbacks.onToolCall?.(parsed);
        break;
      case 'tool_result':
        callbacks.onToolResult?.(parsed);
        break;
      case 'done':
        callbacks.onDone?.(parsed);
        break;
      case 'error':
        callbacks.onError?.(parsed);
        break;
      default:
        // Fallback: check if parsed object contains explicit event property
        if (parsed?.event && parsed?.data) {
          handleGenericEvent(parsed.event, parsed.data, callbacks);
        } else if (parsed?.text) {
          callbacks.onToken?.({ text: parsed.text });
        }
        break;
    }
  };

  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;

      buffer += decoder.decode(value, { stream: true });
      const lines = buffer.split(/\r\n|\r|\n/);
      // Keep incomplete line in buffer
      buffer = lines.pop() ?? '';

      for (const line of lines) {
        if (line === '') {
          // Empty line indicates end of an event block
          dispatchCurrentEvent();
        } else if (line.startsWith(':')) {
          // SSE comment, ignore
          continue;
        } else if (line.startsWith('event:')) {
          currentEvent = line.slice(6).trim();
        } else if (line.startsWith('data:')) {
          currentDataLines.push(line.slice(5).trimStart());
        }
      }
    }

    // Flush any remaining buffered event
    if (buffer.trim()) {
      if (buffer.startsWith('data:')) {
        currentDataLines.push(buffer.slice(5).trimStart());
      }
      dispatchCurrentEvent();
    }
  } catch (err: any) {
    if (signal?.aborted || err?.name === 'AbortError') {
      // User aborted stream
      return;
    }
    callbacks.onError?.({ message: err?.message || '响应流异常中断' });
    throw err;
  } finally {
    reader.releaseLock();
  }
}

function handleGenericEvent(
  event: string,
  data: any,
  callbacks: SSEChatCallbacks
) {
  switch (event) {
    case 'conversation':
      callbacks.onConversation?.(data);
      break;
    case 'token':
      callbacks.onToken?.(data);
      break;
    case 'sentence':
      callbacks.onSentence?.(data);
      break;
    case 'tool_call':
      callbacks.onToolCall?.(data);
      break;
    case 'tool_result':
      callbacks.onToolResult?.(data);
      break;
    case 'done':
      callbacks.onDone?.(data);
      break;
    case 'error':
      callbacks.onError?.(data);
      break;
  }
}

/**
 * Simulates a realistic SSE chat stream for mock/offline demonstration.
 */
async function simulateMockStreamChat({
  conversationId,
  message,
  signal,
  callbacks,
}: StreamChatOptions): Promise<void> {
  const wait = (ms: number) =>
    new Promise<void>((resolve, reject) => {
      if (signal?.aborted) return reject(new DOMException('Aborted', 'AbortError'));
      const timer = setTimeout(resolve, ms);
      signal?.addEventListener(
        'abort',
        () => {
          clearTimeout(timer);
          reject(new DOMException('Aborted', 'AbortError'));
        },
        { once: true }
      );
    });

  const convId = conversationId || 'conv_mock_' + Math.random().toString(36).substring(2, 9);
  callbacks.onConversation?.({ id: convId });

  const startTime = Date.now();
  await wait(180); // first token delay

  const isMusicQuery = /歌|周杰伦|播放|晴天|放一首|音乐|歌曲/.test(message);

  if (isMusicQuery) {
    const tokens1 = '好的，我来在本地曲库中为您检索相关曲目。\n\n'.split('');
    for (const t of tokens1) {
      callbacks.onToken?.({ text: t });
      await wait(30);
    }

    const toolCallId = 'call_fzq_' + Math.random().toString(36).substring(2, 7);
    callbacks.onToolCall?.({
      id: toolCallId,
      name: 'fzq',
      arguments: JSON.stringify({
        queries: message.replace(/^(请|帮我)?(播放|放|听)/, '').trim() || '周杰伦 晴天',
        candidates: 500,
        limit: 3,
      }, null, 2),
    });

    await wait(350);

    callbacks.onToolResult?.({
      id: toolCallId,
      name: 'fzq',
      result: [
        {
          artist: '周杰伦',
          title: '晴天',
          file: '/vol1/1000/Music/周杰伦 - 晴天.mp3',
          score: 98.5,
          duration: '04:29',
        },
        {
          artist: '周杰伦',
          title: '晴天 (Live)',
          file: '/vol1/1000/Music/周杰伦 - 晴天 (Live).flac',
          score: 89.2,
          duration: '04:45',
        },
      ],
    });

    await wait(120);

    const tokens2 = '\n已为您匹配到曲目《晴天》（周杰伦）。正在发送播放指令至音箱端...'.split('');
    for (const t of tokens2) {
      callbacks.onToken?.({ text: t });
      await wait(30);
    }

    const playCallId = 'call_speaker_' + Math.random().toString(36).substring(2, 7);
    callbacks.onToolCall?.({
      id: playCallId,
      name: 'speaker_play',
      arguments: JSON.stringify({
        file: '/vol1/1000/Music/周杰伦 - 晴天.mp3',
        title: '晴天',
        artist: '周杰伦',
        volume: 45,
      }, null, 2),
    });

    await wait(280);

    callbacks.onToolResult?.({
      id: playCallId,
      name: 'speaker_play',
      result: {
        status: 'playing',
        device: '小爱音箱 Pro (客厅)',
        driver: 'xiaomi_miot',
        volume: 45,
      },
    });

    await wait(100);

    const tokens3 = '\n\n🎵 正在通过客厅小爱音箱播放《晴天》（周杰伦），祝您听歌愉快！'.split('');
    for (const t of tokens3) {
      callbacks.onToken?.({ text: t });
      await wait(30);
    }
  } else {
    // Normal agent reasoning
    const text = `收到您的指令：「${message}」。\n\n小爱音箱 AI Agent 已处于在线状态，当前正运行在家庭 NAS 本地容器环境中。您可以让我查询曲库、播放音乐，或通过左侧管理面板查看工具与模型配置。`;
    const tokens = text.split('');
    for (const t of tokens) {
      callbacks.onToken?.({ text: t });
      await wait(25);
    }
  }

  const totalMs = Date.now() - startTime;
  callbacks.onDone?.({
    usage: {
      prompt_tokens: 142,
      completion_tokens: 88,
      total_tokens: 230,
    },
    timings: {
      first_token_ms: 180,
      total_ms: totalMs,
    },
  });
}
