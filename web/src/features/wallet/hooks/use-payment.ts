/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import i18next from 'i18next'
import { useState, useCallback } from 'react'
import { toast } from 'sonner'

import { handleServerError } from '@/lib/handle-server-error'

import {
  calculateAmount,
  calculateAlipayAmount,
  calculateStripeAmount,
  calculateLinuxDOCreditAmount,
  calculateWeChatPayAmount,
  calculateWaffoAmount,
  calculateWaffoPancakeAmount,
  requestPayment,
  requestAlipayPayment,
  getOfficialPaymentStatus,
  requestStripePayment,
  requestLinuxDOCreditPayment,
  requestWeChatPayPayment,
  isApiSuccess,
} from '../api'
import {
  isOfficialAlipayPayment,
  isOfficialWeChatPayPayment,
  isStripePayment,
  isWaffoPayment,
  isWaffoPancakePayment,
  isLinuxDOCreditPayment,
  submitHtmlPaymentForm,
  submitPaymentForm,
} from '../lib'
import type { AmountRequest, AmountResponse } from '../types'

// ============================================================================
// Payment Hook
// ============================================================================

type AmountCalculator = (request: AmountRequest) => Promise<AmountResponse>

export interface PaymentAmountCalculators {
  regular: AmountCalculator
  stripe: AmountCalculator
  waffo: AmountCalculator
  waffoPancake: AmountCalculator
}

const defaultPaymentAmountCalculators: PaymentAmountCalculators = {
  regular: calculateAmount,
  stripe: calculateStripeAmount,
  waffo: calculateWaffoAmount,
  waffoPancake: calculateWaffoPancakeAmount,
}

export async function requestPaymentAmount(
  topupAmount: number,
  paymentType: string,
  calculators: PaymentAmountCalculators = defaultPaymentAmountCalculators
): Promise<number> {
  let calculator = calculators.regular
  if (isStripePayment(paymentType)) {
    calculator = calculators.stripe
  } else if (isWaffoPayment(paymentType)) {
    calculator = calculators.waffo
  } else if (isWaffoPancakePayment(paymentType)) {
    calculator = calculators.waffoPancake
  }

  const response = await calculator({ amount: topupAmount })
  if (!isApiSuccess(response) || !response.data) {
    return 0
  }

  return Number.parseFloat(response.data)
}

export function usePayment() {
  const [amount, setAmount] = useState<number>(0)
  const [calculating, setCalculating] = useState(false)
  const [processing, setProcessing] = useState(false)
  const [qrPayment, setQrPayment] = useState<{
    qrCode: string
    iframeUrl?: string
    orderId?: string
    paymentType: string
  } | null>(null)

  // Calculate payment amount
  const calculatePaymentAmount = useCallback(
    async (topupAmount: number, paymentType: string) => {
      try {
        setCalculating(true)

        const isStripe = isStripePayment(paymentType)
        let response
        if (isStripe) {
          response = await calculateStripeAmount({ amount: topupAmount })
        } else if (isOfficialAlipayPayment(paymentType)) {
          response = await calculateAlipayAmount({ amount: topupAmount })
        } else if (isOfficialWeChatPayPayment(paymentType)) {
          response = await calculateWeChatPayAmount({ amount: topupAmount })
        } else if (isLinuxDOCreditPayment(paymentType)) {
          response = await calculateLinuxDOCreditAmount({ amount: topupAmount })
        } else if (isWaffoPancakePayment(paymentType)) {
          response = await calculateWaffoPancakeAmount({ amount: topupAmount })
        } else {
          response = await calculateAmount({ amount: topupAmount })
        }

        if (isApiSuccess(response) && response.data) {
          const calculatedAmount = Number.parseFloat(response.data)
          setAmount(calculatedAmount)
          return calculatedAmount
        }

        // Don't show error for calculation, just set to 0
        setAmount(0)
        return 0
      } catch {
        setAmount(0)
        return 0
      } finally {
        setCalculating(false)
      }
    },
    []
  )

  // Process payment
  const processPayment = useCallback(
    async (topupAmount: number, paymentType: string) => {
      try {
        setProcessing(true)

        const isStripe = isStripePayment(paymentType)
        const isAlipay = isOfficialAlipayPayment(paymentType)
        const isWeChatPay = isOfficialWeChatPayPayment(paymentType)
        const isLinuxDOCredit = isLinuxDOCreditPayment(paymentType)
        const amount = Math.floor(topupAmount)

        let response
        if (isStripe) {
          response = await requestStripePayment({
            amount,
            payment_method: 'stripe',
          })
        } else if (isAlipay) {
          response = await requestAlipayPayment({
            amount,
            payment_method: paymentType,
          })
        } else if (isWeChatPay) {
          response = await requestWeChatPayPayment({
            amount,
            payment_method: paymentType,
          })
        } else if (isLinuxDOCredit) {
          response = await requestLinuxDOCreditPayment({
            amount,
            payment_method: paymentType,
          })
        } else {
          response = await requestPayment({
            amount,
            payment_method: paymentType,
          })
        }

        if (!isApiSuccess(response)) {
          handleServerError(response, i18next.t('Payment request failed'))
          return false
        }

        // Handle Stripe payment
        if (isStripe && response.data) {
          const data = response.data as { pay_link?: string }
          if (data.pay_link) {
            window.open(data.pay_link, '_blank')
            toast.success(i18next.t('Redirecting to payment page...'))
            return true
          }
        }

        if ((isAlipay || isWeChatPay) && response.data) {
          const data = response.data as {
            pay_form?: string
            pay_url?: string
            qr_code?: string
            order_id?: string
          }
          if (data.pay_form) {
            submitHtmlPaymentForm(data.pay_form)
            toast.success(i18next.t('Redirecting to payment page...'))
            return true
          }
          if (data.pay_url) {
            if (isAlipay) {
              setQrPayment({
                qrCode: '',
                iframeUrl: data.pay_url,
                orderId: data.order_id,
                paymentType,
              })
              toast.success(i18next.t('Scan QR code to pay'))
              return true
            }
            window.open(data.pay_url, '_blank')
            toast.success(i18next.t('Redirecting to payment page...'))
            return true
          }
          if (data.qr_code) {
            setQrPayment({
              qrCode: data.qr_code,
              orderId: data.order_id,
              paymentType,
            })
            toast.success(i18next.t('Scan QR code to pay'))
            return true
          }
        }

        // Handle non-Stripe payment
        if (!isStripe && response.data) {
          const url = (response as unknown as { url?: string }).url
          if (url) {
            submitPaymentForm(url, response.data)
            toast.success(i18next.t('Redirecting to payment page...'))
            return true
          }
        }

        return false
      } catch (error) {
        handleServerError(error, i18next.t('Payment request failed'))
        return false
      } finally {
        setProcessing(false)
      }
    },
    []
  )

  const clearQrPayment = useCallback(() => setQrPayment(null), [])

  const checkQrPaymentStatus = useCallback(async () => {
    if (!qrPayment?.orderId) {
      return null
    }

    const response = await getOfficialPaymentStatus(
      qrPayment.orderId,
      qrPayment.paymentType
    )
    if (isApiSuccess(response) && response.data) {
      return response.data
    }
    if (response.message) {
      throw new Error(response.message)
    }
    return null
  }, [qrPayment])

  return {
    amount,
    calculating,
    processing,
    calculatePaymentAmount,
    processPayment,
    setAmount,
    qrPayment,
    clearQrPayment,
    checkQrPaymentStatus,
  }
}
