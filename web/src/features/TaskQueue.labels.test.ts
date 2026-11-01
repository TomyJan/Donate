import { expect, it } from 'vitest';
import { translate } from '../lib/i18n';
import { taskLabel } from './TaskQueue';

it.each([
  ['database_updated', '属地库已更新', 'IP database updated'],
  ['database_update_failed', '属地库更新失败', 'IP database update failed'],
  ['database_update_interrupted', '属地库更新已中断', 'IP database update interrupted'],
  ['disabled', '任务已禁用', 'Task disabled'],
  ['located_mmdb', '属地已更新', 'Location updated'],
  ['located_xdb', '属地已更新（XDB）', 'Location updated (XDB)'],
  ['located_ip9', '属地已更新（ip9）', 'Location updated (ip9)'],
  ['located_ipyou', '属地已更新（ipyou）', 'Location updated (ipyou)'],
  ['located_i5p', '属地已更新（i5p）', 'Location updated (i5p)'],
  ['located_pconline', '属地已更新（pconline）', 'Location updated (pconline)'],
  ['located_unavailable', '该 IP 无可用属地', 'No location available for this IP'],
  ['located_unknown', '未取得可用属地', 'No usable location obtained'],
  ['close_wait', '关单请求等待中断', 'Close request wait interrupted'],
  ['close_unknown', '远程关单结果未知', 'Remote close result unknown'],
  ['final_query_wait', '关单后核验等待中断', 'Post-close verification wait interrupted'],
  ['final_query_failed', '关单后查询失败', 'Post-close query failed'],
  ['final_validation_failed', '关单后核验结果未通过校验', 'Post-close verification validation failed'],
  ['refunded_skipped', '已退款，跳过关单', 'Refunded; closure skipped'],
  ['frozen_skipped', '已冻结，跳过关单', 'Frozen; closure skipped'],
  ['authorized_skipped', '预授权，跳过关单', 'Authorized; closure skipped'],
  ['unknown_skipped', '支付状态未知，跳过关单', 'Payment status unknown; closure skipped'],
  ['window_ended_refunded', '核验窗口结束，已退款', 'Verification window ended, refunded'],
  ['window_ended_frozen', '核验窗口结束，已冻结', 'Verification window ended, frozen'],
  ['window_ended_authorized', '核验窗口结束，预授权', 'Verification window ended, authorized'],
])('renders known task result %s in Chinese and English', (code, chinese, english) => {
  expect(taskLabel((key) => translate(key, 'zh-CN'), 'result', code)).toBe(chinese);
  expect(taskLabel((key) => translate(key, 'en'), 'result', code)).toBe(english);
});

it.each(['zh-CN', 'en'] as const)('preserves unknown task result codes in %s', (language) => {
  for (const code of ['located_future_provider', 'close_future_status', 'window_ended_future_state']) {
    expect(taskLabel((key) => translate(key, language), 'result', code)).toBe(code);
  }
});
