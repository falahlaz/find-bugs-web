import { useState, type FormEvent } from 'react'
import { useAuth } from '@/app/auth-context'
import { allMenus, menus, type Menu } from '@/app/menus'
import { Alert } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { PageHeader } from '@/components/ui/page-header'
import { Select } from '@/components/ui/select'
import { useTimezone } from '@/features/findbugs/queries'
import type { User } from '@/lib/api'
import { formatDateTime } from '@/lib/format'
import { cn } from '@/lib/utils'
import { useCreateUser, useUpdateUser, useUsers, type Role } from './queries'

/** One toggle per menu; QA only, Engineers always see everything. */
function MenuToggles({ value, onChange, disabled, label }: { value: Menu[]; onChange: (v: Menu[]) => void; disabled?: boolean; label: string }) {
  return (
    <div role="group" aria-label={label} className="flex flex-wrap gap-1">
      {menus.map((m) => {
        const on = value.includes(m.key)
        return (
          <button
            key={m.key}
            type="button"
            aria-pressed={on}
            title={m.hint}
            disabled={disabled}
            onClick={() => onChange(on ? value.filter((v) => v !== m.key) : allMenus.filter((k) => k === m.key || value.includes(k)))}
            className={cn(
              'inline-flex h-7 items-center gap-1 rounded-md border px-2 text-xs font-medium transition-colors disabled:opacity-50',
              on ? 'border-primary bg-primary/10 text-primary' : 'text-muted-foreground line-through decoration-1 hover:bg-secondary',
            )}
          >
            <m.icon className="size-3.5" aria-hidden />
            {m.label}
          </button>
        )
      })}
    </div>
  )
}

function CreateUserForm() {
  const create = useCreateUser()
  const [done, setDone] = useState('')
  const [role, setRole] = useState<Role>('qa')
  const [newMenus, setNewMenus] = useState<Menu[]>(allMenus)

  function onSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const form = e.currentTarget
    const data = new FormData(form)
    const username = String(data.get('username'))
    create.mutate(
      { username, password: String(data.get('password')), role, menus: role === 'qa' ? newMenus : undefined },
      {
        onSuccess: () => {
          setDone(`User ${username} dibuat. Kirim password-nya lewat jalur aman.`)
          form.reset()
          setRole('qa')
          setNewMenus(allMenus)
        },
      },
    )
  }

  return (
    <form onSubmit={onSubmit} className="grid gap-3 sm:grid-cols-4 sm:items-end">
      <div className="grid gap-1.5">
        <Label htmlFor="new-username">Username</Label>
        <Input id="new-username" name="username" required minLength={3} maxLength={64} pattern="[A-Za-z0-9._\-]+" autoComplete="off" />
      </div>
      <div className="grid gap-1.5">
        <Label htmlFor="new-password">Password awal</Label>
        <Input id="new-password" name="password" type="password" required minLength={8} maxLength={72} autoComplete="new-password" />
      </div>
      <div className="grid gap-1.5">
        <Label htmlFor="new-role">Role</Label>
        <Select id="new-role" name="role" value={role} onChange={(e) => setRole(e.target.value as Role)}>
          <option value="qa">QA</option>
          <option value="engineer">Engineer</option>
        </Select>
      </div>
      <Button type="submit" disabled={create.isPending}>
        Tambah user
      </Button>
      {role === 'qa' && (
        <div className="grid gap-1.5 sm:col-span-4">
          <span className="text-sm font-medium">Menu yang dibuka</span>
          <MenuToggles value={newMenus} onChange={setNewMenus} label="Menu user baru" />
        </div>
      )}
      {create.error && (
        <Alert tone="danger" className="sm:col-span-4">
          {create.error.message}
        </Alert>
      )}
      {done && !create.error && (
        <Alert tone="info" className="sm:col-span-4">
          {done}
        </Alert>
      )}
    </form>
  )
}

function UserRow({ u, self }: { u: User; self: boolean }) {
  const tz = useTimezone()
  const update = useUpdateUser()
  const [resetting, setResetting] = useState(false)
  const [password, setPassword] = useState('')

  return (
    <>
      <tr className="border-b last:border-0">
        <td className="py-2 pr-3 font-medium">
          {u.username} {self && <span className="text-xs text-muted-foreground">(kamu)</span>}
        </td>
        <td className="py-2 pr-3">
          <Select
            className="h-8 w-32"
            value={u.role}
            disabled={self || update.isPending}
            onChange={(e) => update.mutate({ id: u.id, role: e.target.value as Role })}
            aria-label={`Role ${u.username}`}
          >
            <option value="qa">QA</option>
            <option value="engineer">Engineer</option>
          </Select>
        </td>
        <td className="py-2 pr-3">
          {u.role === 'engineer' ? (
            <span className="text-xs text-muted-foreground">Semua menu</span>
          ) : (
            <MenuToggles value={u.menus} disabled={update.isPending} onChange={(m) => update.mutate({ id: u.id, menus: m })} label={`Menu ${u.username}`} />
          )}
        </td>
        <td className="py-2 pr-3">{u.active ? <Badge tone="success">Aktif</Badge> : <Badge tone="neutral">Nonaktif</Badge>}</td>
        <td className="whitespace-nowrap py-2 pr-3 text-muted-foreground">{formatDateTime(u.createdAt, tz)}</td>
        <td className="py-2 text-right">
          <div className="flex justify-end gap-2">
            <Button variant="outline" size="sm" onClick={() => setResetting((v) => !v)}>
              Reset password
            </Button>
            {!self && (
              <Button
                variant="outline"
                size="sm"
                disabled={update.isPending}
                onClick={() => {
                  if (!u.active || window.confirm(`Nonaktifkan ${u.username}? Sesi login-nya langsung berakhir.`)) {
                    update.mutate({ id: u.id, active: !u.active })
                  }
                }}
              >
                {u.active ? 'Nonaktifkan' : 'Aktifkan'}
              </Button>
            )}
          </div>
        </td>
      </tr>
      {(resetting || update.error) && (
        <tr className="border-b">
          <td colSpan={6} className="py-2">
            {resetting && (
              <form
                className="flex flex-wrap items-center gap-2"
                onSubmit={(e) => {
                  e.preventDefault()
                  update.mutate(
                    { id: u.id, password },
                    {
                      onSuccess: () => {
                        setPassword('')
                        setResetting(false)
                      },
                    },
                  )
                }}
              >
                <Input
                  type="password"
                  className="h-8 w-64"
                  placeholder="Password baru (min. 8 karakter)"
                  minLength={8}
                  maxLength={72}
                  required
                  autoComplete="new-password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                />
                <Button size="sm" type="submit" disabled={update.isPending}>
                  Simpan
                </Button>
                <span className="text-xs text-muted-foreground">Sesi login user ini akan berakhir.</span>
              </form>
            )}
            {update.error && <p className="text-sm text-destructive">{update.error.message}</p>}
          </td>
        </tr>
      )}
    </>
  )
}

export function UsersPage() {
  const { user } = useAuth()
  const users = useUsers()
  return (
    <div className="grid gap-4">
      <PageHeader title="User" description="Tidak ada self-signup. Setiap perubahan role, status, atau password langsung mengakhiri sesi user tersebut. Perubahan menu QA berlaku tanpa login ulang." />
      <Card>
        <CardHeader>
          <CardTitle>Tambah user</CardTitle>
        </CardHeader>
        <CardContent>
          <CreateUserForm />
        </CardContent>
      </Card>
      <Card>
        <CardHeader>
          <CardTitle>Semua user</CardTitle>
        </CardHeader>
        <CardContent>
          {users.error && <Alert tone="danger">{users.error.message}</Alert>}
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b text-left text-[11px] font-semibold tracking-wide text-muted-foreground uppercase">
                  <th className="py-2 pr-3 font-medium">Username</th>
                  <th className="py-2 pr-3 font-medium">Role</th>
                  <th className="py-2 pr-3 font-medium">Menu</th>
                  <th className="py-2 pr-3 font-medium">Status</th>
                  <th className="py-2 pr-3 font-medium">Dibuat</th>
                  <th className="py-2" />
                </tr>
              </thead>
              <tbody>
                {(users.data ?? []).map((u) => (
                  <UserRow key={u.id} u={u} self={u.id === user?.id} />
                ))}
              </tbody>
            </table>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
