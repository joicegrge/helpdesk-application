import { Routes } from '@angular/router';
import { Dashboard } from './dashboard/dashboard';
import { roleGuard } from './auth.guard';
import { Login } from './login/login';
import { Placeholder } from './placeholder/placeholder';
import { AllTickets } from './tickets/all-tickets';
import { CreateTicket } from './tickets/create-ticket';
import { UserTickets } from './tickets/user-tickets';
import { TicketDetails } from './tickets/ticket-details';

export const routes: Routes = [
  { path: '', pathMatch: 'full', redirectTo: 'dashboard' },
  { path: 'login', component: Login },
  { path: 'dashboard', component: Dashboard, canActivate: [roleGuard], data: { roles: ['ADMIN', 'SUPPORT'] } },
  { path: 'tickets/create', component: CreateTicket, canActivate: [roleGuard], data: { roles: ['ADMIN', 'SUPPORT', 'USER'] } },
  { path: 'tickets/:ticketNumber', component: TicketDetails, canActivate: [roleGuard], data: { roles: ['ADMIN', 'SUPPORT', 'USER'] } },
  { path: 'tickets', component: AllTickets, canActivate: [roleGuard], data: { roles: ['ADMIN'] } },
  { path: 'my-tickets', component: UserTickets, canActivate: [roleGuard], data: { ticketView: 'mine', roles: ['USER'] } },
  { path: 'assigned', component: UserTickets, canActivate: [roleGuard], data: { ticketView: 'assigned', roles: ['SUPPORT'] } },
  { path: '**', redirectTo: 'dashboard' },
];
