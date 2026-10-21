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
  duration_ms?: number;
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
}

/**
 * Parses raw SSE chunks into event/data pairs.
 */
export async function streamChat({
  conversationId,
  message,
  signal,
  callbacks,
}: StreamChatOptions): Promise<void> {
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
