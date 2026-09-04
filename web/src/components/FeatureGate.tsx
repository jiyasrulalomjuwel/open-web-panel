import { useEffect, useState } from 'react';
import { ShieldAlert, Eye } from 'lucide-react';
import { getChildAccount, type FeatureName } from '../lib/api';

// Module-level cache so every gate on a page shares one /child/account fetch.
let cached: Record<string, boolean> | null = null;
let inflight: Promise<Record<string, boolean>> | null = null;

export function fetchFeatures(): Promise<Record<string, boolean>> {
  if (cached) return Promise.resolve(cached);
  if (!inflight) {
    inflight = getChildAccount()
      .then((a: any) => {
        cached = (a?.features as Record<string, boolean>) || {};
        return cached;
      })
      .catch(() => ({} as Record<string, boolean>))
      .finally(() => { inflight = null; }) as Promise<Record<string, boolean>>;
  }
  return inflight;
}

// Drop cached gates on logout/account switch (api.ts clearTokens dispatches
// owp:logout) so the next session never inherits the previous user's access.
if (typeof window !== 'undefined') {
  window.addEventListener('owp:logout', () => { cached = null; inflight = null; });
}

// Fail-open while loading (the API still enforces); fail-open when the
// account payload predates the features field.
export function useFeatureAccess(feature: FeatureName): { allowed: boolean; loading: boolean } {
  const [state, setState] = useState({ allowed: true, loading: cached == null });
  useEffect(() => {
    let live = true;
    fetchFeatures().then((f) => {
      if (live) setState({ allowed: f[feature] !== false, loading: false });
    });
    return () => { live = false; };
  }, [feature]);
  return state;
}

// Emails disabled = read-only inbox: reads stay allowed, send/manage blocked.
export function useEmailsReadOnly(): { readOnly: boolean; loading: boolean } {
  const { allowed, loading } = useFeatureAccess('emails');
  return { readOnly: !allowed, loading };
}

export function FeatureDisabledNotice({ feature, title }: { feature: string; title: string }) {
  return (
    <div className="flex items-center justify-center py-16">
      <div className="text-center max-w-sm">
        <ShieldAlert className="h-10 w-10 mx-auto mb-3 text-gray-300 dark:text-gray-600" />
        <h2 className="text-base font-semibold text-gray-900 dark:text-gray-100">{title} Disabled</h2>
        <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">
          Your administrator has disabled {feature} for your account. Contact support if you need access.
        </p>
      </div>
    </div>
  );
}

export function ReadOnlyBanner({ text }: { text: string }) {
  return (
    <div className="flex items-center gap-2 p-3 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg text-sm text-blue-700 dark:text-blue-300">
      <Eye className="h-4 w-4 shrink-0" />{text}
    </div>
  );
}

// Route-level gate for fully-blocked features (files, ftp, db, backups).
export function FeatureRoute({ feature, title, children }: { feature: FeatureName; title: string; children: React.ReactNode }) {
  const { allowed, loading } = useFeatureAccess(feature);
  if (loading) {
    return <div className="flex items-center justify-center py-16 text-gray-400 text-sm">Loading...</div>;
  }
  if (!allowed) return <FeatureDisabledNotice feature={feature} title={title} />;
  return <>{children}</>;
}
