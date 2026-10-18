import { HttpErrorResponse } from '@angular/common/http';
import { Component, inject, signal } from '@angular/core';
import { FormControl, FormGroup, ReactiveFormsModule, Validators } from '@angular/forms';
import { Router } from '@angular/router';
import { TimeoutError } from 'rxjs';
import { AuthService } from '../auth.service';

@Component({
  selector: 'app-login',
  imports: [ReactiveFormsModule],
  templateUrl: './login.html',
  styleUrl: './login.css',
})
export class Login {
  protected readonly passwordVisible = signal(false);
  protected readonly submitted = signal(false);
  protected readonly notice = signal('');
  protected readonly noticeType = signal<'info' | 'error' | 'success'>('info');
  protected readonly loading = signal(false);

  private readonly authService = inject(AuthService);
  private readonly router = inject(Router);

  protected readonly loginForm = new FormGroup({
    email: new FormControl('', {
      nonNullable: true,
      validators: [Validators.required, Validators.email],
    }),
    password: new FormControl('', {
      nonNullable: true,
      validators: [Validators.required],
    }),
    rememberMe: new FormControl(false, { nonNullable: true }),
  });

  protected submit(): void {
    if (this.loading()) return;
    this.submitted.set(true);
    this.notice.set('');

    if (this.loginForm.invalid) {
      this.loginForm.markAllAsTouched();
      return;
    }

    const rememberMe = this.loginForm.controls.rememberMe.value;
    this.loading.set(true);
    this.authService
      .login(this.loginForm.controls.email.value.trim(), this.loginForm.controls.password.value, rememberMe)
      .subscribe({
        next: (response) => {
          const sessionSaved = this.authService.saveSession(response, rememberMe);
          this.loading.set(false);
          if (!sessionSaved) {
            this.noticeType.set('error');
            this.notice.set('Your browser could not save the sign-in session. Check its storage settings and try again.');
            return;
          }
          void this.router.navigateByUrl('/dashboard');
        },
        error: (error: HttpErrorResponse) => {
          this.loading.set(false);
          this.noticeType.set('error');
          if (error instanceof TimeoutError) {
            this.notice.set('The HelpDesk server took too long to respond. Check that it is running and try again.');
          } else if (error.status === 401) {
            this.notice.set('The email or password you entered is incorrect.');
          } else if (error.status === 0) {
            this.notice.set('Could not reach the HelpDesk server. Check that the backend is running and try again.');
          } else if (error.status === 400) {
            this.notice.set('Enter your email address and password, then try again.');
          } else {
            this.notice.set('Sign in is temporarily unavailable. Please try again.');
          }
        },
      });
  }

  protected showRecoveryMessage(event: Event): void {
    event.preventDefault();
    this.noticeType.set('info');
    this.notice.set('Password recovery will be available later.');
  }

  protected showEmailError(): string {
    const email = this.loginForm.controls.email;
    if (!(email.touched || this.submitted())) return '';
    if (email.hasError('required')) return 'Please enter your email address.';
    if (email.hasError('email')) return 'Enter a valid email address, like name@company.com.';
    return '';
  }
}
