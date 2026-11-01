import type { Profile } from './types';

export const sessionIdentity = (profile: Profile | null | undefined) =>
  JSON.stringify([profile?.id, profile?.csrf_token, profile?.admin]);

export const isSessionQuery = (key: readonly unknown[]) =>
  key[0] !== 'site' && key[0] !== 'profile' && key[0] !== 'login-flow';
