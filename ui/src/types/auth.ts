export type User = {
  id: string;
  email: string;
  name: string;
  picture: string;
  createdAt: string;
};

export type Session = {
  token: string;
  expiresAt: string;
  user: User;
  created: boolean;
};

export type Credential = {
  id: string;
  name: string;
  createdAt: string;
  lastUsedAt: string | null;
};
