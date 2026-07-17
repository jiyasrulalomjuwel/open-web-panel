import { useState } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import { Server, Globe, Mail, Check, ArrowRight, ArrowLeft, Rocket } from 'lucide-react';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';
import { useToast } from '../components/ToastProvider';

const API_BASE = '/api/v1';

async function request(url: string, opts?: any) {
  const token = localStorage.getItem('owp_access_token');
  const res = await fetch(API_BASE + url, {
    ...opts,
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}`, ...opts?.headers },
    body: opts?.body ? JSON.stringify(opts.body) : undefined,
  });
  if (!res.ok) throw new Error(await res.text());
  return res.json();
}

const steps = [
  { title: 'Welcome', icon: Server },
  { title: 'Nameservers', icon: Globe },
  { title: 'Admin Email', icon: Mail },
  { title: 'Done', icon: Check },
];

export function Onboarding() {
  const { toast } = useToast();
  const [step, setStep] = useState(0);
  const [saving, setSaving] = useState(false);

  // Step 1: Welcome / Server hostname
  const [hostname, setHostname] = useState('');
  const [serverIp, setServerIp] = useState('');

  // Step 2: Nameservers
  const [ns1, setNs1] = useState('');
  const [ns2, setNs2] = useState('');

  // Step 3: Admin email
  const [adminEmail, setAdminEmail] = useState('');

  const handleNext = async () => {
    if (step === 0) {
      if (!hostname) { toast('error', 'Please enter a hostname'); return; }
      setSaving(true);
      try {
        await request('/settings', {
          method: 'PUT',
          body: { server_hostname: hostname, server_ip: serverIp }
        });
      } catch (e: any) {
        toast('error', e.message || 'Failed to save');
        setSaving(false);
        return;
      }
      setSaving(false);
    }

    if (step === 1) {
      if (!ns1) { toast('error', 'Please enter at least one nameserver'); return; }
      setSaving(true);
      try {
        await request('/settings', {
          method: 'PUT',
          body: { ns1, ns2 }
        });
      } catch (e: any) {
        toast('error', e.message || 'Failed to save');
        setSaving(false);
        return;
      }
      setSaving(false);
    }

    if (step === 2) {
      if (!adminEmail) { toast('error', 'Please enter an admin email'); return; }
      setSaving(true);
      try {
        await request('/settings', {
          method: 'PUT',
          body: { admin_email: adminEmail }
        });
        await request('/setup/complete', { method: 'POST' });
      } catch (e: any) {
        toast('error', e.message || 'Failed to save');
        setSaving(false);
        return;
      }
      setSaving(false);
    }

    if (step < steps.length - 1) {
      setStep(step + 1);
    }
  };

  const handleFinish = () => {
    window.location.href = '/';
  };

  return (
    <div className="min-h-screen bg-gray-50 dark:bg-gray-950 flex flex-col items-center justify-center p-4">
      <div className="w-full max-w-lg">
        {/* Step indicator */}
        <div className="flex items-center justify-center mb-8">
          {steps.map((s, i) => (
            <div key={s.title} className="flex items-center">
              <div className={`flex items-center justify-center w-8 h-8 rounded-full text-xs font-medium transition-colors ${
                i <= step
                  ? 'bg-blue-600 text-white'
                  : 'bg-gray-200 dark:bg-gray-700 text-gray-400 dark:text-gray-500'
              }`}>
                {i < step ? <Check size={14} /> : i + 1}
              </div>
              {i < steps.length - 1 && (
                <div className={`w-12 h-0.5 mx-1 transition-colors ${
                  i < step ? 'bg-blue-600' : 'bg-gray-200 dark:bg-gray-700'
                }`} />
              )}
            </div>
          ))}
        </div>

        <AnimatePresence mode="wait">
          <motion.div
            key={step}
            initial={{ opacity: 0, x: 20 }}
            animate={{ opacity: 1, x: 0 }}
            exit={{ opacity: 0, x: -20 }}
            transition={{ duration: 0.2 }}
          >
            <Card className="p-8">
              {step === 0 && (
                <div className="space-y-6">
                  <div className="text-center">
                    <div className="inline-flex items-center justify-center w-16 h-16 bg-blue-50 dark:bg-blue-900/30 rounded-full mb-4">
                      <Rocket className="h-8 w-8 text-blue-600 dark:text-blue-400" />
                    </div>
                    <h2 className="text-xl font-semibold text-gray-900 dark:text-gray-100">Welcome to OpenWebPanel</h2>
                    <p className="text-sm text-gray-500 dark:text-gray-400 mt-2">Let's configure your server in a few steps</p>
                  </div>
                  <div>
                    <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Server Hostname</label>
                    <input type="text" value={hostname} onChange={e => setHostname(e.target.value)}
                      className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
                      placeholder="server.example.com" />
                  </div>
                  <div>
                    <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Shared IP Address</label>
                    <input type="text" value={serverIp} onChange={e => setServerIp(e.target.value)}
                      className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
                      placeholder="203.0.113.1" />
                  </div>
                </div>
              )}

              {step === 1 && (
                <div className="space-y-6">
                  <div className="text-center">
                    <div className="inline-flex items-center justify-center w-16 h-16 bg-blue-50 dark:bg-blue-900/30 rounded-full mb-4">
                      <Globe className="h-8 w-8 text-blue-600 dark:text-blue-400" />
                    </div>
                    <h2 className="text-xl font-semibold text-gray-900 dark:text-gray-100">Nameserver Configuration</h2>
                    <p className="text-sm text-gray-500 dark:text-gray-400 mt-2">Set the nameservers for your hosting platform</p>
                  </div>
                  <div>
                    <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Nameserver 1</label>
                    <input type="text" value={ns1} onChange={e => setNs1(e.target.value)}
                      className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
                      placeholder="ns1.example.com" />
                  </div>
                  <div>
                    <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Nameserver 2</label>
                    <input type="text" value={ns2} onChange={e => setNs2(e.target.value)}
                      className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
                      placeholder="ns2.example.com" />
                  </div>
                </div>
              )}

              {step === 2 && (
                <div className="space-y-6">
                  <div className="text-center">
                    <div className="inline-flex items-center justify-center w-16 h-16 bg-blue-50 dark:bg-blue-900/30 rounded-full mb-4">
                      <Mail className="h-8 w-8 text-blue-600 dark:text-blue-400" />
                    </div>
                    <h2 className="text-xl font-semibold text-gray-900 dark:text-gray-100">Admin Email</h2>
                    <p className="text-sm text-gray-500 dark:text-gray-400 mt-2">Set the admin email for system notifications</p>
                  </div>
                  <div>
                    <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Email Address</label>
                    <input type="email" value={adminEmail} onChange={e => setAdminEmail(e.target.value)}
                      className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
                      placeholder="admin@example.com" />
                  </div>
                </div>
              )}

              {step === 3 && (
                <div className="text-center space-y-4 py-4">
                  <div className="inline-flex items-center justify-center w-16 h-16 bg-emerald-50 dark:bg-emerald-900/30 rounded-full mb-2">
                    <Check className="h-8 w-8 text-emerald-600 dark:text-emerald-400" />
                  </div>
                  <h2 className="text-xl font-semibold text-gray-900 dark:text-gray-100">All Set!</h2>
                  <p className="text-sm text-gray-500 dark:text-gray-400">
                    Your server has been configured. You can now start creating hosting accounts and managing your server.
                  </p>
                </div>
              )}

              <div className="flex items-center justify-between mt-8 pt-6 border-t border-gray-200 dark:border-gray-700">
                <Button
                  variant="ghost"
                  onClick={() => setStep(Math.max(0, step - 1))}
                  disabled={step === 0}
                >
                  <ArrowLeft size={16} /> Back
                </Button>

                {step < steps.length - 1 ? (
                  <Button onClick={handleNext} loading={saving}>
                    Next <ArrowRight size={16} />
                  </Button>
                ) : (
                  <Button onClick={handleFinish}>
                    Go to Dashboard <ArrowRight size={16} />
                  </Button>
                )}
              </div>
            </Card>
          </motion.div>
        </AnimatePresence>

        <p className="text-center text-xs text-gray-400 dark:text-gray-500 mt-6">
          OpenWebPanel Setup Wizard
        </p>
      </div>
    </div>
  );
}
