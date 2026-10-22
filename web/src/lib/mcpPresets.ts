import type { McpServer } from './api';

export interface McpPreset {
  id: string;
  desc: string;
  needs?: 'uv' | 'bun';
  key?: boolean;
  server: Partial<McpServer>;
}

export const MCP_PRESETS: McpPreset[] = [
  { id: 'time', desc: '时间与时区换算', needs: 'uv',
    server: { transport: 'stdio', command: 'uvx', args: ['mcp-server-time', '--local-timezone=Asia/Shanghai'] } },
  { id: 'fetch', desc: '抓取网页内容转为文本', needs: 'uv',
    server: { transport: 'stdio', command: 'uvx', args: ['mcp-server-fetch'] } },
  { id: 'duckduckgo', desc: '网页搜索（免密钥；国内网络可能不可用）', needs: 'uv',
    server: { transport: 'stdio', command: 'uvx', args: ['duckduckgo-mcp-server'] } },
  { id: 'memory', desc: '长期记忆（知识图谱，存到 /data/workspace/memory.jsonl）', needs: 'bun',
    server: { transport: 'stdio', command: 'bunx', args: ['@modelcontextprotocol/server-memory'], env: { MEMORY_FILE_PATH: '/data/workspace/memory.jsonl' } } },
  { id: 'filesystem', desc: '读写 /data/workspace 与只读曲库 /music', needs: 'bun',
    server: { transport: 'stdio', command: 'bunx', args: ['@modelcontextprotocol/server-filesystem', '/data/workspace', '/music'] } },
  { id: 'amap', desc: '高德地图：天气、路线、地点（需免费 Key，console.amap.com 申请）', key: true,
    server: { transport: 'http', url: 'https://mcp.amap.com/mcp?key=<你的高德 Key>' } },
  { id: 'homeassistant', desc: 'Home Assistant 智能家居控制（HA 需启用 MCP Server 集成，长期访问令牌）', key: true,
    server: { transport: 'http', url: 'http://<HA地址>:8123/api/mcp', headers: { Authorization: 'Bearer <长期访问令牌>' } } },
];
