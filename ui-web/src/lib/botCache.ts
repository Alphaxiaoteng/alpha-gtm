import { mutate } from 'swr';

/**
 * Bot 有两套投影缓存：
 *   - `api/agent-bots`       名册（设置页 / 详情面板读）
 *   - `api/agent-bots/rail`  侧栏（分组 / 未读 / 置顶标记读）
 *
 * 任何写到这两个投影共享字段（pin / primary / 分组 / 免打扰 / 增删）的操作，
 * 都必须同时失效两者，否则侧栏图标和顺序会停在旧值。
 */
export const BOT_CACHE_KEYS = ['api/agent-bots', 'api/agent-bots/rail'] as const;

export async function revalidateBotCaches(): Promise<void> {
  await Promise.all(BOT_CACHE_KEYS.map((key) => mutate(key)));
}
