import React, { useState, useEffect } from "react";
import { Button } from "@/components/ui/button";
import { Plus, ChevronLeft, ChevronRight, Check, User, Shield, Lock, Eye, Settings, Code, Database as DatabaseIcon, Edit3 } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
  DialogFooter,
} from "@/components/ui/dialog";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Form } from "@/components/ui/form";
import { toast } from "sonner";
import { useForm } from "react-hook-form";
import useAppStore from "@/store";
import { format } from "date-fns";
import { generateRandomPassword } from "@/lib/utils";
import AuthenticationSection from "./AuthenticationSection";
import DatabaseRolesSection from "./DatabaseRolesSection";
import useMetadata from "./hooks/useMetadata";
import { motion, AnimatePresence } from "framer-motion";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Label } from "@/components/ui/label";
import { GlassCard, GlassCardContent } from "@/components/ui/glass-card";

interface CreateNewUserProps {
  onUserCreated: () => void;
}

const STEPS = [
  { id: 0, title: "Identity", icon: User },
  { id: 1, title: "Role & Scope", icon: Shield },
  { id: 2, title: "Review", icon: Check },
];

const CreateNewUser: React.FC<CreateNewUserProps> = ({ onUserCreated }) => {
  const [isOpen, setIsOpen] = useState(false);
  const [currentStep, setCurrentStep] = useState(0);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const [onCluster, setOnCluster] = useState(false);
  const [clusterName, setClusterName] = useState("");
  const [accessPreset, setAccessPreset] = useState("read_only"); // admin, developer, read_write, read_only

  const form = useForm({
    defaultValues: {
      username: "",
      password: "",
      grantDatabases: [],
      // Keep simplified defaults
      hostType: "ANY",
      hostValue: "",
      validUntil: undefined,
      defaultRole: "",
      defaultDatabase: "",
      settings: {
        profile: "",
        readonly: false,
      },
    },
  });

  const metadata = useMetadata(isOpen);
  const { runQuery, credential } = useAppStore();

  useEffect(() => {
    if (credential?.isDistributed && credential?.clusterName) {
      setOnCluster(true);
      setClusterName(credential.clusterName);
    }
  }, [credential]);

  // Reset form when closed
  useEffect(() => {
    if (!isOpen) {
      setCurrentStep(0);
      form.reset();
      setAccessPreset("read_only");
    }
  }, [isOpen, form]);


  const onSubmit = async () => {
    const data = form.getValues();
    try {
      setError("");
      setLoading(true);

      // Validation
      if (accessPreset !== "admin" && data.grantDatabases.length === 0) {
        setError("Please select at least one database for this role.");
        setLoading(false);
        return;
      }

      const createUserQuery = buildUserCreationQuery(data);
      const grantQueries = buildGrantQueries(data.username, data, accessPreset);

      // Execute Create User
      const createResult = await runQuery(createUserQuery);
      if (createResult.error) {
        throw new Error(createResult.error);
      }

      // Execute Grants
      for (const query of grantQueries) {
        const result = await runQuery(query);
        if (result.error) {
          // Rollback attempt (best effort)
          await runQuery(`DROP USER IF EXISTS ${data.username}`);
          throw new Error(`Failed to grant privileges: ${result.error}`);
        }
      }

      // Apply Readonly Setting if needed (Though Profile is better, we'll use SETTINGS READONLY=1 for strict safety)
      if (accessPreset === "read_only") {
        await runQuery(`ALTER USER ${data.username} SETTINGS READONLY=1`);
      }

      toast.success(`User ${data.username} created successfully`);
      onUserCreated();
      setIsOpen(false);
    } catch (err: any) {
      setError(err.message || "Failed to create user");
    } finally {
      setLoading(false);
    }
  };

  const handleGeneratePassword = () => {
    const newPassword = generateRandomPassword();
    form.setValue("password", newPassword);
  };

  const buildUserCreationQuery = (data: any) => {
    let query = `CREATE USER IF NOT EXISTS ${data.username}`;
    if (onCluster && clusterName) query += ` ON CLUSTER ${clusterName}`;
    query += ` IDENTIFIED WITH sha256_password BY '${data.password}'`;
    // Defaults for simplified flow
    query += ` HOST ANY`;
    return query;
  };

  const buildGrantQueries = (username: string, data: any, preset: string) => {
    const queries: string[] = [];

    if (preset === "admin") {
      queries.push(`GRANT ALL ON *.* TO ${username} WITH GRANT OPTION`);
      return queries;
    }

    const dbs = data.grantDatabases as string[];

    for (const db of dbs) {
      if (preset === "developer") {
        // Developer: Full control over objects + Data
        queries.push(`GRANT CREATE, DROP, ALTER, SELECT, INSERT, DELETE, TRUNCATE, OPTIMIZE ON ${db}.* TO ${username}`);
      } else if (preset === "read_write") {
        // Read-Write: Data manipulation only
        queries.push(`GRANT SELECT, INSERT ON ${db}.* TO ${username}`);
      } else if (preset === "read_only") {
        // Read-Only: Select only
        queries.push(`GRANT SELECT ON ${db}.* TO ${username}`);
      }
    }

    // Default system access for exceptions dashboard
    queries.push(`GRANT SELECT ON system.query_log TO ${username}`);

    return queries;
  };

  const nextStep = async () => {
    if (currentStep === 0) {
      const valid = await form.trigger(['username', 'password']);
      if (!valid) return;
    }
    setCurrentStep(prev => Math.min(prev + 1, STEPS.length - 1));
  };

  const prevStep = () => setCurrentStep(prev => Math.max(prev - 1, 0));

  return (
    <Dialog open={isOpen} onOpenChange={setIsOpen}>
      <DialogTrigger asChild>
        <Button className="gap-2 bg-gradient-to-r from-purple-600 to-blue-600 hover:from-purple-500 hover:to-blue-500 text-white shadow-lg shadow-purple-500/20 border border-white/10">
          <Plus className="h-4 w-4" />
          Create New User
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-3xl bg-[#0f1115] border-white/10 text-white max-h-[90vh] overflow-hidden flex flex-col p-0 gap-0 shadow-2xl shadow-black/50">

        {/* Header */}
        <DialogHeader className="p-6 pb-4 border-b border-white/5 bg-black/20">
          <div className="flex justify-between items-center mb-4">
            <DialogTitle className="text-xl font-bold bg-clip-text text-transparent bg-gradient-to-r from-white to-gray-400">
              Create New User
            </DialogTitle>
          </div>

          {/* Stepper */}
          <div className="flex items-center w-full max-w-md mx-auto relative">
            <div className="absolute top-1/2 left-0 w-full h-[2px] bg-white/10 -z-10" />
            <div
              className="absolute top-1/2 left-0 h-[2px] bg-purple-500 -z-10 transition-all duration-300"
              style={{ width: `${(currentStep / (STEPS.length - 1)) * 100}%` }}
            />
            {STEPS.map((s, idx) => {
              const Icon = s.icon;
              const isActive = idx === currentStep;
              const isCompleted = idx < currentStep;
              return (
                <div key={idx} className="flex-1 flex flex-col items-center gap-2">
                  <div
                    className={`
                          w-10 h-10 rounded-full flex items-center justify-center border-2 transition-all duration-300 bg-[#0f1115]
                          ${isActive ? 'border-purple-500 text-purple-400 shadow-[0_0_15px_rgba(168,85,247,0.3)]' :
                        isCompleted ? 'border-green-500 text-green-400 bg-green-500/10' : 'border-white/10 text-gray-600'}
                      `}
                  >
                    {isCompleted ? <Check className="w-5 h-5" /> : <Icon className="w-5 h-5" />}
                  </div>
                  <span className={`text-xs font-medium transition-colors duration-300 ${isActive ? 'text-white' : 'text-gray-500'}`}>
                    {s.title}
                  </span>
                </div>
              );
            })}
          </div>
        </DialogHeader>

        {/* Content */}
        <div className="flex-1 overflow-y-auto p-8 min-h-[400px]">
          <Form {...form}>
            <form className="space-y-6 h-full">
              <AnimatePresence mode="wait">

                {/* STEP 0: IDENTITY */}
                {currentStep === 0 && (
                  <motion.div
                    key="step0"
                    initial={{ opacity: 0, x: 20 }}
                    animate={{ opacity: 1, x: 0 }}
                    exit={{ opacity: 0, x: -20 }}
                    className="max-w-md mx-auto space-y-6"
                  >
                    <div className="text-center space-y-2 mb-8">
                      <h2 className="text-2xl font-bold text-white">Who is this user?</h2>
                      <p className="text-gray-400">Set the login credentials for the new user.</p>
                    </div>

                    <AuthenticationSection form={form} handleGeneratePassword={handleGeneratePassword} />
                  </motion.div>
                )}

                {/* STEP 1: ROLE & SCOPE */}
                {currentStep === 1 && (
                  <motion.div
                    key="step1"
                    initial={{ opacity: 0, x: 20 }}
                    animate={{ opacity: 1, x: 0 }}
                    exit={{ opacity: 0, x: -20 }}
                    className="space-y-8"
                  >
                    <div className="text-center space-y-2">
                      <h2 className="text-2xl font-bold text-white">Assign a Role</h2>
                      <p className="text-gray-400">Choose the level of access and permission scope.</p>
                    </div>

                    <RadioGroup value={accessPreset} onValueChange={setAccessPreset} className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
                      {/* Admin */}
                      <Label
                        className={`
                            relative flex flex-col gap-4 p-6 rounded-xl border-2 cursor-pointer transition-all duration-200
                            bg-white/5 hover:bg-white/10
                            ${accessPreset === 'admin' ? 'border-purple-500 bg-purple-500/10' : 'border-white/5'}
                          `}
                      >
                        <RadioGroupItem value="admin" className="sr-only" />
                        <div className={`p-3 rounded-lg w-fit ${accessPreset === 'admin' ? 'bg-purple-500 text-white' : 'bg-white/10 text-gray-400'}`}>
                          <Shield className="w-6 h-6" />
                        </div>
                        <div>
                          <h3 className="text-lg font-bold text-white mb-1">Administrator</h3>
                          <p className="text-xs text-gray-400 leading-relaxed">
                            Full access to all databases, users, and system settings.
                          </p>
                        </div>
                      </Label>

                      {/* Developer */}
                      <Label
                        className={`
                            relative flex flex-col gap-4 p-6 rounded-xl border-2 cursor-pointer transition-all duration-200
                            bg-white/5 hover:bg-white/10
                            ${accessPreset === 'developer' ? 'border-blue-500 bg-blue-500/10' : 'border-white/5'}
                          `}
                      >
                        <RadioGroupItem value="developer" className="sr-only" />
                        <div className={`p-3 rounded-lg w-fit ${accessPreset === 'developer' ? 'bg-blue-500 text-white' : 'bg-white/10 text-gray-400'}`}>
                          <Code className="w-6 h-6" />
                        </div>
                        <div>
                          <h3 className="text-lg font-bold text-white mb-1">Developer</h3>
                          <p className="text-xs text-gray-400 leading-relaxed">
                            Can create tables, drop objects, and manage data in specific databases.
                          </p>
                        </div>
                      </Label>

                      {/* Read-Write */}
                      <Label
                        className={`
                            relative flex flex-col gap-4 p-6 rounded-xl border-2 cursor-pointer transition-all duration-200
                            bg-white/5 hover:bg-white/10
                            ${accessPreset === 'read_write' ? 'border-orange-500 bg-orange-500/10' : 'border-white/5'}
                          `}
                      >
                        <RadioGroupItem value="read_write" className="sr-only" />
                        <div className={`p-3 rounded-lg w-fit ${accessPreset === 'read_write' ? 'bg-orange-500 text-white' : 'bg-white/10 text-gray-400'}`}>
                          <Edit3 className="w-6 h-6" />
                        </div>
                        <div>
                          <h3 className="text-lg font-bold text-white mb-1">Read-Write</h3>
                          <p className="text-xs text-gray-400 leading-relaxed">
                            Can insert and query data, but cannot modify table structure.
                          </p>
                        </div>
                      </Label>

                      {/* Read-Only */}
                      <Label
                        className={`
                            relative flex flex-col gap-4 p-6 rounded-xl border-2 cursor-pointer transition-all duration-200
                            bg-white/5 hover:bg-white/10
                            ${accessPreset === 'read_only' ? 'border-green-500 bg-green-500/10' : 'border-white/5'}
                          `}
                      >
                        <RadioGroupItem value="read_only" className="sr-only" />
                        <div className={`p-3 rounded-lg w-fit ${accessPreset === 'read_only' ? 'bg-green-500 text-white' : 'bg-white/10 text-gray-400'}`}>
                          <Eye className="w-6 h-6" />
                        </div>
                        <div>
                          <h3 className="text-lg font-bold text-white mb-1">Read-Only</h3>
                          <p className="text-xs text-gray-400 leading-relaxed">
                            Can only Select/Query data. No write access allowed.
                          </p>
                        </div>
                      </Label>
                    </RadioGroup>

                    {/* Conditional Database Selection */}
                    {accessPreset !== 'admin' && (
                      <GlassCard className="animate-in fade-in slide-in-from-bottom-4">
                        <GlassCardContent>
                          <div className="flex items-center gap-3 mb-6">
                            <div className="p-2 rounded-lg bg-white/10">
                              <DatabaseIcon className="w-5 h-5 text-purple-400" />
                            </div>
                            <div>
                              <h3 className="font-semibold text-white">Scope</h3>
                              <p className="text-xs text-gray-400">Select which databases this user can access.</p>
                            </div>
                          </div>
                          <DatabaseRolesSection form={form} roles={[]} databases={metadata.databases} />
                        </GlassCardContent>
                      </GlassCard>
                    )}
                  </motion.div>
                )}

                {/* STEP 2: REVIEW */}
                {currentStep === 2 && (
                  <motion.div
                    key="step2"
                    initial={{ opacity: 0, x: 20 }}
                    animate={{ opacity: 1, x: 0 }}
                    exit={{ opacity: 0, x: -20 }}
                    className="max-w-2xl mx-auto space-y-8"
                  >
                    <div className="text-center space-y-2">
                      <h2 className="text-2xl font-bold text-white">Review & Create</h2>
                      <p className="text-gray-400">Verify the details and the generated SQL before confirming.</p>
                    </div>

                    <div className="grid grid-cols-2 gap-4">
                      <div className="p-4 rounded-xl bg-white/5 border border-white/5">
                        <span className="text-xs text-gray-500 uppercase font-bold tracking-wider">Username</span>
                        <div className="text-lg font-mono text-white mt-1">{form.getValues().username}</div>
                      </div>
                      <div className="p-4 rounded-xl bg-white/5 border border-white/5">
                        <span className="text-xs text-gray-500 uppercase font-bold tracking-wider">Role</span>
                        <div className="text-lg font-mono text-white mt-1 capitalize">{accessPreset.replace('_', ' ')}</div>
                      </div>
                    </div>

                    <div className="space-y-4">
                      <div className="flex items-center justify-between">
                        <Label className="text-sm font-medium text-gray-400">Generated SQL Preview</Label>
                      </div>
                      <div className="relative rounded-xl overflow-hidden border border-white/10 bg-black/50 shadow-inner">
                        <pre className="p-6 text-sm font-mono text-green-400 overflow-x-auto whitespace-pre-wrap leading-relaxed">
                          {buildUserCreationQuery(form.getValues())};
                          {'\n\n'}
                          {buildGrantQueries(form.getValues().username, form.getValues(), accessPreset).join(';\n')};
                          {accessPreset === 'read_only' && `\n\nALTER USER ${form.getValues().username} SETTINGS READONLY=1;`}
                        </pre>
                      </div>
                    </div>

                    {error && (
                      <Alert variant="destructive" className="animate-in zoom-in border-red-500/50 bg-red-900/20">
                        <AlertDescription className="text-red-200 font-medium text-center">
                          {error}
                        </AlertDescription>
                      </Alert>
                    )}
                  </motion.div>
                )}

              </AnimatePresence>
            </form>
          </Form>
        </div>

        {/* Footer */}
        <DialogFooter className="p-6 border-t border-white/10 bg-white/5 flex gap-3">
          <Button
            variant="ghost"
            onClick={prevStep}
            disabled={currentStep === 0 || loading}
            className="text-gray-400 hover:text-white hover:bg-white/5"
          >
            <ChevronLeft className="w-4 h-4 mr-1" /> Back
          </Button>

          {currentStep < 2 ? (
            <Button
              onClick={nextStep}
              className="bg-white text-black hover:bg-gray-200 min-w-[120px]"
            >
              Next <ChevronRight className="w-4 h-4 ml-1" />
            </Button>
          ) : (
            <Button
              onClick={form.handleSubmit(onSubmit)}
              disabled={loading}
              className="bg-gradient-to-r from-purple-600 to-blue-600 hover:from-purple-500 hover:to-blue-500 text-white min-w-[140px] shadow-lg shadow-purple-500/20"
            >
              {loading ? (
                <div className="flex items-center gap-2">
                  <span className="h-4 w-4 animate-spin rounded-full border-2 border-white border-t-transparent" />
                  Creating...
                </div>
              ) : (
                <>Create User <Check className="w-4 h-4 ml-2" /></>
              )}
            </Button>
          )}
        </DialogFooter>

      </DialogContent>
    </Dialog>
  );
};

export default CreateNewUser;
