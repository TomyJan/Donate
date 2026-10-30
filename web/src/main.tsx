import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MotionConfig } from 'motion/react';
import React from 'react';
import { createRoot } from 'react-dom/client';
import { BrowserRouter, Route, Routes } from 'react-router';
import { AppProvider } from './app/context';
import { Layout } from './app/Layout';

const Admin = React.lazy(() => import('./features/Admin').then((m) => ({ default: m.Admin })));

import { CompleteLogin } from './features/CompleteLogin';
import { Home } from './features/Home';
import { LegalPage } from './features/LegalPage';
import './styles/shell.css';
import './styles/home.css';
import './styles/admin.css';
import './styles/app.css';

const client = new QueryClient({
  defaultOptions: {
    queries: { retry: 1, staleTime: 15000, refetchOnWindowFocus: false },
    mutations: { retry: false },
  },
});
const root = document.getElementById('root');
if (!root) throw new Error('Root missing');
createRoot(root).render(
  <React.StrictMode>
    <QueryClientProvider client={client}>
      <BrowserRouter>
        <MotionConfig reducedMotion="user">
          <AppProvider>
            <Routes>
              <Route element={<Layout />}>
                <Route index element={<Home />} />
                <Route path="admin" element={<Admin />} />
                <Route path="auth/complete" element={<CompleteLogin />} />
                <Route path="terms" element={<LegalPage kind="terms" />} />
                <Route path="privacy" element={<LegalPage kind="privacy" />} />
              </Route>
            </Routes>
          </AppProvider>
        </MotionConfig>
      </BrowserRouter>
    </QueryClientProvider>
  </React.StrictMode>,
);
