import type { Operation } from './types';

export function locationResultKey(result: string, status?: string) {
  if (result.startsWith('located_')) return 'locationUpdated';
  if (result === 'disabled' || result === 'not_found') return result;
  if (status === 'failed') return 'locationRefreshFailed';
  if (result === 'source_unavailable') return 'locationRetry';
  return result || 'ready';
}

export function operationItemResultKey(operation: Operation, item: Operation['items'][number]) {
  if (operation.action === 'refresh_location') {
    if (operation.confirmed || item.result === 'disabled' || item.result === 'not_found')
      return locationResultKey(item.result, item.status);
  }
  return operation.confirmed ? item.result : (item.status ?? item.result);
}
