import type { components } from '../generated/api';

type Schemas = components['schemas'];
export type Identity = Schemas['Identity'];
export type Profile = Schemas['Profile'];
export type Site = Schemas['Site'];
export type RecordItem = Schemas['RecordItem'];
export type Records = Schemas['Records'];
export type Order = Schemas['Order'];
export type User = Schemas['User'];
export type Operation = Schemas['Operation'];
export type Audit = Schemas['Audit'];
export type Metric = Schemas['Metric'];
export type Overview = Schemas['Overview'];
export type Page<T> = { items: T[]; total: number; page: number; page_size: number };
