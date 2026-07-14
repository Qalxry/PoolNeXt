export interface AuthUser {
  id: number;
  username: string;
  display_name: string;
  role: number;
  status: number;
  must_change_password: boolean;
  email?: string;
  github_id?: string;
  wechat_id?: string;
}

export interface LoginPayload {
  username: string;
  password: string;
}

export interface RegisterPayload {
  username: string;
  password: string;
  email?: string;
  verification_code?: string;
}

export interface PasswordResetRequestPayload {
  email: string;
  token: string;
}

export interface GitHubOAuthStart {
  authorize_url: string;
}

export interface GitHubOAuthResult {
  user: AuthUser;
  redirect: string;
  mode: 'login' | 'bind';
}
