import type { Site } from './types';

export type LoginProvider = NonNullable<Site['provider_details']>[number];

export function loginProviders(site?: Site): LoginProvider[] {
  return (site?.providers ?? []).map(
    (id) =>
      site?.provider_details?.find((provider) => provider.id === id) ?? {
        id,
        kind: id,
        name: id.toUpperCase(),
        icon_url: '',
      },
  );
}

export function providerForKind(kind: string, site?: Site): LoginProvider {
  return (
    site?.provider_details?.find((provider) => provider.kind === kind) ?? {
      id: kind,
      kind,
      name: kind.toUpperCase(),
      icon_url: '',
    }
  );
}
