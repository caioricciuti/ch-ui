import React, { useState, useEffect } from "react";
import { Button } from "@/components/ui/button";
import { Check, Shield, Network } from "lucide-react";
import {
    Dialog,
    DialogContent,
    DialogHeader,
    DialogTitle,
    DialogFooter,
} from "@/components/ui/dialog";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Form } from "@/components/ui/form";
import { toast } from "sonner";
import { useForm } from "react-hook-form";
import useAppStore from "@/store";
import DatabaseRolesSection from "../CreateUser/DatabaseRolesSection";
import AccessControlSection from "../CreateUser/AccessControlSection";
import useMetadata from "../CreateUser/hooks/useMetadata";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Label } from "@/components/ui/label";
import { GlassCard, GlassCardContent } from "@/components/ui/glass-card";

import { UserData } from "@/features/admin/types";

interface EditUserProps {
    user: UserData | null;
    isOpen: boolean;
    onClose: () => void;
    onUserUpdated: () => void;
}

const STEPS = [
    { id: 0, title: "Role & Scope", icon: Shield },
    { id: 1, title: "Network", icon: Network },
    { id: 2, title: "Review", icon: Check },
];

const EditUser: React.FC<EditUserProps> = ({ user, isOpen, onClose, onUserUpdated }) => {
    const [currentStep, setCurrentStep] = useState(0);
    const [error, setError] = useState("");
    const [loading, setLoading] = useState(false);
    const [accessPreset, setAccessPreset] = useState("read_only");

    const form = useForm({
        defaultValues: {
            grantDatabases: [] as string[],
            defaultRole: "",
            defaultDatabase: "",
            hostType: "ANY",
            hostValue: "",
            validUntil: undefined as Date | undefined,
        },
    });

    // Safe Metadata Hook
    const metadata = useMetadata(isOpen);
    const { runQuery, credential } = useAppStore();

    useEffect(() => {
        if (isOpen && user) {
            setCurrentStep(0);

            // Map Host Access
            let hType = "ANY";
            let hValue = "";

            if (user.host_ip && user.host_ip.length > 0) {
                hType = "IP";
                hValue = user.host_ip.join(", ");
            } else if (user.host_names && user.host_names.length > 0) {
                hType = "NAME";
                hValue = user.host_names.join(", ");
            } else if (user.host_names_regexp && user.host_names_regexp.length > 0) {
                hType = "REGEXP";
                hValue = user.host_names_regexp.join(", ");
            } else if (user.host_names_like && user.host_names_like.length > 0) {
                hType = "LIKE";
                hValue = user.host_names_like.join(", ");
            }

            form.reset({
                grantDatabases: [],
                defaultRole: "",
                defaultDatabase: user.default_database || "",
                hostType: hType,
                hostValue: hValue,
                validUntil: undefined, // TODO: Parse existing profile valid_until if needed, but complex
            });
            // Defer logic to avoid render loop
            setTimeout(() => determinePresetFromGrants(user.grants || []), 0);
        }
    }, [isOpen, user]);

    const determinePresetFromGrants = (grants: string[]) => {
        const grantsString = grants.join(" ").toUpperCase();

        if (/GRANT\s+(ALL|ALL\s+PRIVILEGES)\s+ON\s+\*\.\*/.test(grantsString) ||
            grantsString.includes("ROLE ADMIN") ||
            (grantsString.includes("CREATE USER") && grantsString.includes("DROP USER"))) {
            setAccessPreset("admin");
            form.setValue("grantDatabases", []);
        } else if (grantsString.includes("CREATE") && grantsString.includes("DROP")) {
            setAccessPreset("developer");
            extractDatabasesFromGrants(grants);
        } else if (grantsString.includes("INSERT")) {
            setAccessPreset("read_write");
            extractDatabasesFromGrants(grants);
        } else {
            setAccessPreset("read_only");
            extractDatabasesFromGrants(grants);
        }
    };

    const extractDatabasesFromGrants = (grants: string[]) => {
        const dbs = new Set<string>();
        grants.forEach(g => {
            const match = g.match(/ON\s+"?([a-zA-Z0-9_]+)"?\.(\*|\S+)/);
            if (match && match[1] && match[1] !== '*') {
                dbs.add(match[1]);
            }
        });
        form.setValue("grantDatabases", Array.from(dbs));
    };

    const getHostQuery = (username: string, data: any) => {
        const onClusterClause = credential?.isDistributed && credential?.clusterName ? ` ON CLUSTER '${credential.clusterName}'` : "";
        const prefix = `ALTER USER ${username}${onClusterClause}`;

        if (!data.hostType || data.hostType === "ANY") return `${prefix} HOST ANY`;
        if (data.hostType === "LOCAL") return `${prefix} HOST LOCAL`;

        // Sanitize and split values
        const values = data.hostValue.split(',').map((v: string) => `'${v.trim()}'`).join(", ");

        if (data.hostType === "IP") return `${prefix} HOST IP ${values}`;
        if (data.hostType === "NAME") return `${prefix} HOST NAME ${values}`;
        if (data.hostType === "REGEXP") return `${prefix} HOST REGEXP ${values}`;
        if (data.hostType === "LIKE") return `${prefix} HOST LIKE ${values}`;

        return `${prefix} HOST ANY`;
    };

    const onSubmit = async () => {
        if (!user) return;
        const data = form.getValues();

        try {
            setError("");
            setLoading(true);

            if (accessPreset !== "admin" && data.grantDatabases.length === 0) {
                setError("Please select at least one database for this role.");
                setLoading(false);
                return;
            }

            // 1. Host Access
            const hostQuery = getHostQuery(user.name, data);
            await runQuery(hostQuery);

            const onClusterClause = credential?.isDistributed && credential?.clusterName ? ` ON CLUSTER '${credential.clusterName}'` : "";

            // 2. Refresh Grants (Revoke All + Re-Grant)
            await runQuery(`REVOKE ON CLUSTER '${credential?.clusterName}' ALL ON *.* FROM ${user.name}`);
            // Wait, logic above: if not distributed, empty string. But here I hardcoded it with check?
            // Let's be consistent.
            // Actually REVOKE grammar: REVOKE [ON CLUSTER cluster] privileges...

            const revokeQuery = credential?.isDistributed && credential?.clusterName
                ? `REVOKE ON CLUSTER '${credential.clusterName}' ALL ON *.* FROM ${user.name}`
                : `REVOKE ALL ON *.* FROM ${user.name}`;

            await runQuery(revokeQuery);

            const grantQueries = buildGrantQueries(user.name, data, accessPreset);
            for (const query of grantQueries) {
                const result = await runQuery(query);
                if (result.error) throw new Error(`Failed to grant privileges: ${result.error}`);
            }

            // 3. Settings
            const isReadOnly = accessPreset === "read_only" ? 1 : 0;
            const alterSettingsQuery = credential?.isDistributed && credential?.clusterName
                ? `ALTER USER ${user.name} ON CLUSTER '${credential.clusterName}' SETTINGS READONLY=${isReadOnly}`
                : `ALTER USER ${user.name} SETTINGS READONLY=${isReadOnly}`;

            await runQuery(alterSettingsQuery);

            toast.success(`User ${user.name} updated successfully`);
            onUserUpdated();
            onClose();
        } catch (err: any) {
            setError(err.message || "Failed to update user");
        } finally {
            setLoading(false);
        }
    };

    const buildGrantQueries = (username: string, data: any, preset: string) => {
        const queries: string[] = [];
        const onClusterClause = credential?.isDistributed && credential?.clusterName ? ` ON CLUSTER '${credential.clusterName}'` : "";

        if (preset === "admin") {
            queries.push(`GRANT${onClusterClause} ALL ON *.* TO ${username} WITH GRANT OPTION`);
            return queries;
        }
        const dbs = data.grantDatabases as string[];
        for (const db of dbs) {
            if (preset === "developer") queries.push(`GRANT${onClusterClause} CREATE, DROP, ALTER, SELECT, INSERT, DELETE, TRUNCATE, OPTIMIZE ON ${db}.* TO ${username}`);
            else if (preset === "read_write") queries.push(`GRANT${onClusterClause} SELECT, INSERT ON ${db}.* TO ${username}`);
            else if (preset === "read_only") queries.push(`GRANT${onClusterClause} SELECT ON ${db}.* TO ${username}`);
        }

        // Default access to exceptions for everyone
        queries.push(`GRANT${onClusterClause} SELECT ON system.query_log TO ${username}`);
        return queries;
    };

    if (!user) return null;

    return (
        <Dialog open={isOpen} onOpenChange={(open) => !open && onClose()}>
            <DialogContent className="sm:max-w-3xl bg-[#0f1115] border-white/10 text-white max-h-[90vh] overflow-hidden flex flex-col p-0 gap-0 shadow-2xl shadow-black/50">

                {/* Header */}
                <DialogHeader className="p-6 pb-4 border-b border-white/5 bg-black/20">
                    <div className="flex justify-between items-center mb-4">
                        <DialogTitle className="text-xl font-bold bg-clip-text text-transparent bg-gradient-to-r from-white to-gray-400">
                            Edit User: {user.name}
                        </DialogTitle>
                    </div>
                    <div className="flex items-center w-full max-w-md mx-auto relative">
                        <div className="absolute top-1/2 left-0 w-full h-[2px] bg-white/10 -z-10" />
                        <div className="absolute top-1/2 left-0 h-[2px] bg-purple-500 -z-10 transition-all duration-300" style={{ width: `${(currentStep / (STEPS.length - 1)) * 100}%` }} />
                        {STEPS.map((s, idx) => {
                            const Icon = s.icon;
                            return (
                                <div key={idx} className="flex-1 flex flex-col items-center gap-2">
                                    <div className={`w-10 h-10 rounded-full flex items-center justify-center border-2 transition-all duration-300 bg-[#0f1115] ${idx === currentStep ? 'border-purple-500 text-purple-400' : idx < currentStep ? 'border-green-500 text-green-400' : 'border-white/10 text-gray-600'}`}>
                                        {idx < currentStep ? <Check className="w-5 h-5" /> : <Icon className="w-5 h-5" />}
                                    </div>
                                    <span className="text-xs font-medium text-gray-500">{s.title}</span>
                                </div>
                            );
                        })}
                    </div>
                </DialogHeader>

                <div className="flex-1 overflow-y-auto p-8 min-h-[400px]">
                    <Form {...form}>
                        <form className="space-y-6 h-full">
                            {/* Step 0: Role & Scope */}
                            {currentStep === 0 && (
                                <div className="space-y-8">
                                    <RadioGroup value={accessPreset} onValueChange={setAccessPreset} className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
                                        {['admin', 'developer', 'read_write', 'read_only'].map(preset => (
                                            <Label key={preset} className={`relative flex flex-col gap-4 p-6 rounded-xl border-2 cursor-pointer transition-all duration-200 bg-white/5 hover:bg-white/10 ${accessPreset === preset ? 'border-purple-500 bg-purple-500/10' : 'border-white/5'}`}>
                                                <RadioGroupItem value={preset} className="sr-only" />
                                                <div className="capitalize font-bold text-white">{preset.replace('_', ' ')}</div>
                                            </Label>
                                        ))}
                                    </RadioGroup>
                                    {accessPreset !== 'admin' && (
                                        <GlassCard>
                                            <GlassCardContent>
                                                <DatabaseRolesSection form={form} roles={[]} databases={metadata.databases} />
                                            </GlassCardContent>
                                        </GlassCard>
                                    )}
                                </div>
                            )}

                            {/* Step 1: Network */}
                            {currentStep === 1 && (
                                <div className="max-w-2xl mx-auto">
                                    <GlassCard>
                                        <GlassCardContent>
                                            <AccessControlSection form={form} />
                                        </GlassCardContent>
                                    </GlassCard>
                                </div>
                            )}

                            {/* Step 2: Review */}
                            {currentStep === 2 && (
                                <div className="max-w-2xl mx-auto space-y-8">
                                    <div className="space-y-4">
                                        <Label>Updates Preview (SQL)</Label>
                                        <div className="relative rounded-xl overflow-hidden border border-white/10 bg-black/50 shadow-inner">
                                            <pre className="p-6 text-sm font-mono text-green-400 overflow-x-auto whitespace-pre-wrap leading-relaxed">
                                                {getHostQuery(user.name, form.getValues())};
                                                {'\n'}
                                                REVOKE ALL ON *.* FROM {user.name};
                                                {'\n'}
                                                {buildGrantQueries(user.name, form.getValues(), accessPreset).join(';\n')};
                                                {accessPreset === 'read_only' ? `\n\nALTER USER ${user.name} SETTINGS READONLY=1;` : `\n\nALTER USER ${user.name} SETTINGS READONLY=0;`}
                                            </pre>
                                        </div>
                                    </div>
                                    {error && <Alert variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}
                                </div>
                            )}
                        </form>
                    </Form>
                </div>

                <DialogFooter className="p-6 border-t border-white/10 bg-white/5 flex gap-3">
                    <Button variant="ghost" onClick={() => setCurrentStep(prev => Math.max(prev - 1, 0))} disabled={currentStep === 0 || loading} className="text-gray-400 hover:text-white">Back</Button>
                    {currentStep < 2 ? (
                        <Button onClick={() => setCurrentStep(prev => Math.min(prev + 1, STEPS.length - 1))} className="bg-white text-black hover:bg-gray-200">Next</Button>
                    ) : (
                        <Button onClick={form.handleSubmit(onSubmit)} disabled={loading} className="bg-gradient-to-r from-purple-600 to-blue-600 text-white">
                            {loading ? "Updating..." : "Update User"}
                        </Button>
                    )}
                </DialogFooter>

            </DialogContent>
        </Dialog>
    );
};

export default EditUser;
