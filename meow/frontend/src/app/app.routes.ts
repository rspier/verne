import { Routes } from '@angular/router';
import { LoginComponent } from './login/login';
import { ChatComponent } from './chat/chat';
import { AuthGuard } from './auth.guard';

export const routes: Routes = [
  { path: 'login', component: LoginComponent },
  { path: 'chat', component: ChatComponent, canActivate: [AuthGuard] },
  { path: '', redirectTo: '/chat', pathMatch: 'full' },
];
